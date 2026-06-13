package picker

import (
	"context"
	"errors"
	"sync"

	"aqua/internal/riot"
)

// Snapshot is a game-agnostic view of the current state, produced by a Source.
// The picker turns it into the wire `state`. Phase ∈ "menus"|"lobby"|"queue"|
// "matchfound"|"pregame"|"ingame"; Running=false means the game isn't up (→ offline).
type Snapshot struct {
	Running              bool
	Phase                string
	MatchID              string
	MapID                string
	QueueID              string
	PhaseTimeRemainingNS int64
	Players              []PlayerSlot // ally team during pregame
	OwnedAgents          []string
	Locale               string
}

// PlayerSlot is one ally-team seat in agent select.
type PlayerSlot struct {
	PUUID          string
	CharacterID    string
	SelectionState string // ""|selected|locked
	Name           string
}

// Source is everything the picker needs from "the game". Implemented by
// riotSource (live) and simSource (testing, no live match).
type Source interface {
	Snapshot(ctx context.Context) (Snapshot, error)
	Select(ctx context.Context, matchID, agentID string) error
	Lock(ctx context.Context, matchID, agentID string) error
	Authenticate(ctx context.Context) error // force a fresh auth (test_auth)
	PUUID() string
}

// riotSource adapts the riot.Client to Source with lazy, self-healing auth.
type riotSource struct {
	mu           sync.Mutex
	client       *riot.Client
	owned        []string
	ownedFetched bool
}

// NewRiotSource returns a live game source.
func NewRiotSource() Source { return &riotSource{} }

func (s *riotSource) ensure(ctx context.Context) error {
	if s.client != nil {
		return nil
	}
	c, err := riot.Authenticate(ctx)
	if err != nil {
		return err
	}
	s.client = c
	s.owned, s.ownedFetched = nil, false
	return nil
}

func (s *riotSource) reset() {
	s.client = nil
	s.owned, s.ownedFetched = nil, false
}

func (s *riotSource) PUUID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return ""
	}
	return s.client.PUUID()
}

func (s *riotSource) Authenticate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	return s.ensure(ctx)
}

func (s *riotSource) Snapshot(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, err := s.snapshotLocked(ctx)
	if errors.Is(err, riot.ErrUnauthorized) {
		// The access token expired mid-session (Riot returns 400 BAD_CLAIMS
		// after a while in a match). Drop the stale client, re-auth from the
		// local API, and try once more so the poll heals without surfacing an
		// error to the phone.
		s.reset()
		snap, err = s.snapshotLocked(ctx)
	}
	return snap, err
}

// snapshotLocked does one read pass. Auth errors propagate to Snapshot, which
// re-authenticates and retries. Caller must hold s.mu.
func (s *riotSource) snapshotLocked(ctx context.Context) (Snapshot, error) {
	if err := s.ensure(ctx); err != nil {
		if errors.Is(err, riot.ErrLockfileNotFound) {
			s.reset()
			return Snapshot{Running: false}, nil // game not running → offline
		}
		return Snapshot{}, err
	}
	c := s.client

	// Owned agents change rarely; fetch once per auth (best-effort).
	if !s.ownedFetched {
		if owned, err := c.OwnedAgents(ctx); err == nil {
			s.owned, s.ownedFetched = owned, true
		}
	}

	matchID, err := c.PregamePlayer(ctx)
	if err != nil && !errors.Is(err, riot.ErrNotFound) {
		return Snapshot{}, err // includes ErrUnauthorized → Snapshot re-auths + retries
	}
	if err == nil && matchID != "" {
		m, err := c.PregameMatch(ctx, matchID)
		if err != nil {
			return Snapshot{}, err
		}
		snap := Snapshot{
			Running:              true,
			Phase:                "pregame",
			MatchID:              matchID,
			MapID:                m.MapID,
			QueueID:              m.QueueID,
			PhaseTimeRemainingNS: m.PhaseTimeRemainingNS,
			OwnedAgents:          s.owned,
			Locale:               c.Locale(),
		}
		for _, p := range m.AllyTeam.Players {
			snap.Players = append(snap.Players, PlayerSlot{
				PUUID:          p.Subject,
				CharacterID:    p.CharacterID,
				SelectionState: p.CharacterSelectionState,
			})
		}
		return snap, nil
	}

	// Not in pregame → check for an active match.
	inGame, err := c.InCoreGame(ctx)
	if err != nil {
		return Snapshot{}, err // ErrUnauthorized → Snapshot re-auths + retries
	}
	if inGame {
		return Snapshot{Running: true, Phase: "ingame", OwnedAgents: s.owned, Locale: c.Locale()}, nil
	}

	// Pre-match menus territory. Refine menus/lobby/queue/matchfound from the
	// party (best-effort; the plan says degrade to plain "menus" if it breaks).
	phase, queueID := "menus", ""
	if pid, perr := c.CurrentParty(ctx); perr == nil && pid != "" {
		if pi, perr := c.Party(ctx, pid); perr == nil {
			phase, queueID = partyPhase(pi)
		}
	}
	return Snapshot{Running: true, Phase: phase, QueueID: queueID, OwnedAgents: s.owned, Locale: c.Locale()}, nil
}

// partyPhase maps a party's matchmaking state to the pre-match wire phase.
// ready-check up (or already matchmade) → matchfound; actively searching →
// queue; a queue picked but idle → lobby; nothing picked → bare menus.
func partyPhase(pi riot.PartyInfo) (phase, queueID string) {
	queueID = pi.QueueID
	switch {
	case pi.ReadyCheck == "InProgress" || pi.State == "MATCHMADE":
		return "matchfound", queueID
	case pi.State == "MATCHMAKING":
		return "queue", queueID
	case queueID != "":
		return "lobby", queueID
	default:
		return "menus", queueID
	}
}

func (s *riotSource) Select(ctx context.Context, matchID, agentID string) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return errors.New("not authenticated")
	}
	return c.Select(ctx, matchID, agentID)
}

func (s *riotSource) Lock(ctx context.Context, matchID, agentID string) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return errors.New("not authenticated")
	}
	return c.Lock(ctx, matchID, agentID)
}
