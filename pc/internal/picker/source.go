package picker

import (
	"context"
	"errors"
	"sync"
	"time"

	"aqua/internal/riot"
)

// recentMatchesN is how many recent matches we weigh per player for the
// tracker aggregates (Win%/K-D/ADR/HS%). Kept small: PD endpoints are
// rate-limited and "recent form" is what the scoreboard wants, not a career.
const recentMatchesN = 8

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

// PlayerSlot is one player the game put in front of us — an ally seat in agent
// select, or (with Team set) any of the ten seats in a live match.
type PlayerSlot struct {
	PUUID          string
	CharacterID    string
	SelectionState string            // ""|selected|locked (agent select)
	Name           string            // resolved Game#Tag (filled by the stats fetch)
	Team           string            // "ally"|"enemy" (live match scoreboard)
	Stats          *riot.PlayerStats // tracker row; nil until the background fetch fills it
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

	// Tracker stats are filled by a one-shot background fetch per match so the
	// poll loop never blocks on the slow, rate-limited PD calls. Guarded by its
	// own mutex (the goroutine writes outside the poll's mu).
	statsMu       sync.Mutex
	statsKey      string                      // match id the cache is for
	statsByPUUID  map[string]riot.PlayerStats // nil until the fetch completes
	statsFetching bool
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
		puuids := make([]string, 0, len(m.AllyTeam.Players))
		for _, p := range m.AllyTeam.Players {
			snap.Players = append(snap.Players, PlayerSlot{
				PUUID:          p.Subject,
				CharacterID:    p.CharacterID,
				SelectionState: p.CharacterSelectionState,
				Team:           "ally",
			})
			puuids = append(puuids, p.Subject)
		}
		attachStats(snap.Players, s.lobbyStats(c, matchID, "", puuids))
		return snap, nil
	}

	// Not in pregame → check for an active match (both teams visible here).
	cgMatchID, err := c.CoreGamePlayer(ctx)
	if err != nil && !errors.Is(err, riot.ErrNotFound) {
		return Snapshot{}, err // ErrUnauthorized → Snapshot re-auths + retries
	}
	if err == nil && cgMatchID != "" {
		snap := Snapshot{Running: true, Phase: "ingame", MatchID: cgMatchID, OwnedAgents: s.owned, Locale: c.Locale()}
		seats, serr := c.CoreGameMatch(ctx, cgMatchID)
		if serr != nil {
			return snap, nil // degrade to a bare "in match" screen on any roster failure
		}
		selfTeam := ""
		for _, p := range seats {
			if p.Subject == c.PUUID() {
				selfTeam = p.TeamID
			}
		}
		puuids := make([]string, 0, len(seats))
		for _, p := range seats {
			team := "enemy"
			if p.TeamID == selfTeam {
				team = "ally"
			}
			snap.Players = append(snap.Players, PlayerSlot{
				PUUID:       p.Subject,
				CharacterID: p.CharacterID,
				Team:        team,
			})
			puuids = append(puuids, p.Subject)
		}
		attachStats(snap.Players, s.lobbyStats(c, cgMatchID, "", puuids))
		return snap, nil
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

// lobbyStats returns cached tracker rows for the given match, kicking off a
// one-shot background fetch when the match changes. It returns whatever is
// cached right now — empty while a fetch is in flight — so the poll never
// blocks; the next poll (~1 Hz) picks up the filled cache and re-emits. Caller
// need not hold s.mu (this guards its own state).
func (s *riotSource) lobbyStats(c *riot.Client, matchID, queue string, puuids []string) map[string]riot.PlayerStats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	if matchID != s.statsKey {
		// New match → drop the old cache and (below) refetch.
		s.statsKey, s.statsByPUUID, s.statsFetching = matchID, nil, false
	}
	if matchID != "" && len(puuids) > 0 && s.statsByPUUID == nil && !s.statsFetching {
		s.statsFetching = true
		ps := append([]string(nil), puuids...) // own a copy for the goroutine
		key := matchID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res := c.LobbyStats(ctx, ps, queue, recentMatchesN)
			s.statsMu.Lock()
			if s.statsKey == key { // ignore if the match changed mid-fetch
				s.statsByPUUID = res
			}
			s.statsFetching = false
			s.statsMu.Unlock()
		}()
	}

	out := make(map[string]riot.PlayerStats, len(s.statsByPUUID))
	for k, v := range s.statsByPUUID {
		out[k] = v
	}
	return out
}

// attachStats fills each slot's Name + Stats from a (possibly empty) stats map.
func attachStats(players []PlayerSlot, stats map[string]riot.PlayerStats) {
	for i := range players {
		st, ok := stats[players[i].PUUID]
		if !ok {
			continue
		}
		cp := st
		players[i].Stats = &cp
		if players[i].Name == "" {
			players[i].Name = st.Name
		}
	}
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
