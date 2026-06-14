package picker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"aqua/internal/riot"
)

// recentMatchesN is how many recent matches we weigh per player for the
// tracker aggregates (Win%/K-D/ADR/HS%). Kept small: PD endpoints are
// rate-limited and "recent form" is what the scoreboard wants, not a career.
const recentMatchesN = 8

// partyMatchesN is how many recent matches per player we scan to infer premades
// (the shared-match co-occurrence heuristic in riot.DetectParties). Larger than
// recentMatchesN — detection recall improves with more history — but still just
// one match-history fetch per player, and shared match-details are cached.
const partyMatchesN = 25

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

	// Live round score (ingame only), read from the local presence blob. Ally is
	// our team's rounds. HasScore guards the 0-0 ambiguity: a real pistol round is
	// 0-0, so the bool — not a zero value — says whether to show the score.
	ScoreAlly  int
	ScoreEnemy int
	HasScore   bool

	// Party (lobby) surface — populated in the pre-match states only.
	PartyID          string
	Accessibility    string // OPEN|CLOSED
	InviteCode       string
	MaxPartySize     int
	IsOwner          bool  // the local player owns the party
	QueueEntryMillis int64 // matchmaking start (unix millis), 0 when not queuing
	PartyMembers     []PartySlot
}

// PartySlot is one pre-match party member (the snapshot view; the wire shape is
// picker.PartyMember in state.go). Distinct from PlayerSlot: no agent, but it
// carries ownership + ready state.
type PartySlot struct {
	PUUID   string
	Name    string
	IsOwner bool
	IsReady bool
	Self    bool
	Tier    int
	Stats   *riot.PlayerStats
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
	PartyGroup     int               // 0 = no detected premade; 1..n = inferred party group (per match)
}

// Source is everything the picker needs from "the game". Implemented by
// riotSource (live) and simSource (testing, no live match).
type Source interface {
	Snapshot(ctx context.Context) (Snapshot, error)
	Select(ctx context.Context, matchID, agentID string) error
	Lock(ctx context.Context, matchID, agentID string) error
	Authenticate(ctx context.Context) error // force a fresh auth (test_auth)
	PUUID() string

	// Party (lobby) management. id is the current party id; owner-only operations
	// are gated by the picker before these are called.
	GenerateInviteCode(ctx context.Context, id string) error
	DisableInviteCode(ctx context.Context, id string) error
	JoinByCode(ctx context.Context, code string) error
	LeaveParty(ctx context.Context) error
	KickMember(ctx context.Context, puuid string) error
	SetAccessibility(ctx context.Context, id string, open bool) error
	ChangeQueue(ctx context.Context, id, queueID string) error
	StartMatchmaking(ctx context.Context, id string) error
	StopMatchmaking(ctx context.Context, id string) error
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

	// Inferred party groups (premade detection), same once-per-roster background
	// pattern as stats. partyByPUUID maps puuid → group id (1..n; absent = solo).
	// Keyed by a roster signature so it recomputes when the set of players changes
	// (pregame's 5 allies → core-game's 10), not on every poll.
	partyMu       sync.Mutex
	partySig      string
	partyByPUUID  map[string]int
	partyFetching bool

	// onUpdate is fired when a background stats fetch fills the cache, so the
	// picker re-emits the now-complete scoreboard without waiting for the
	// reconcile tick. Set once via SetOnUpdate before Run; nil until then.
	onUpdate func()
}

// NewRiotSource returns a live game source.
func NewRiotSource() Source { return &riotSource{} }

// SetOnUpdate registers a callback fired when an async stats fetch completes with
// new rows. The picker wires this to its refresh trigger so freshly-loaded tracker
// stats reach the phone immediately. Must be set before Run (no synchronization).
func (s *riotSource) SetOnUpdate(fn func()) { s.onUpdate = fn }

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
		attachPartyGroups(snap.Players, s.partyGroups(c, matchID, snap.Players))
		return snap, nil
	}

	// Not in pregame → check for an active match (both teams visible here).
	cgMatchID, err := c.CoreGamePlayer(ctx)
	if err != nil && !errors.Is(err, riot.ErrNotFound) {
		return Snapshot{}, err // ErrUnauthorized → Snapshot re-auths + retries
	}
	if err == nil && cgMatchID != "" {
		snap := Snapshot{Running: true, Phase: "ingame", MatchID: cgMatchID, OwnedAgents: s.owned, Locale: c.Locale()}
		// Live round score from our own presence — the only live in-match number
		// Riot exposes. Best-effort: absent → no score header on the scoreboard.
		if score := c.LiveMatchScore(ctx); score.Valid {
			snap.ScoreAlly, snap.ScoreEnemy, snap.HasScore = score.Ally, score.Enemy, true
		}
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
		attachPartyGroups(snap.Players, s.partyGroups(c, cgMatchID, snap.Players))
		return snap, nil
	}

	// Pre-match menus territory. Refine menus/lobby/queue/matchfound from the
	// party (best-effort; the plan says degrade to plain "menus" if it breaks),
	// and carry the lobby surface (members, code, accessibility) for the phone.
	snap := Snapshot{Running: true, Phase: "menus", OwnedAgents: s.owned, Locale: c.Locale()}
	if pid, perr := c.CurrentParty(ctx); perr == nil && pid != "" {
		if pi, perr := c.Party(ctx, pid); perr == nil {
			snap.Phase, snap.QueueID = partyPhase(pi)
			snap.PartyID = pid
			snap.Accessibility = pi.Accessibility
			snap.InviteCode = pi.InviteCode
			snap.MaxPartySize = pi.MaxMembers
			snap.QueueEntryMillis = pi.QueueEntryMillis
			puuids := make([]string, 0, len(pi.Members))
			for _, m := range pi.Members {
				self := m.PUUID == c.PUUID()
				if self {
					snap.IsOwner = m.IsOwner
				}
				snap.PartyMembers = append(snap.PartyMembers, PartySlot{
					PUUID: m.PUUID, IsOwner: m.IsOwner, IsReady: m.IsReady, Self: self, Tier: m.Tier,
				})
				puuids = append(puuids, m.PUUID)
			}
			attachPartyStats(snap.PartyMembers, s.lobbyStats(c, pid, snap.QueueID, puuids))
		}
	}
	return snap, nil
}

// attachPartyStats fills each member's Name + Stats from a (possibly empty) stats
// map (the party-member counterpart of attachStats for PlayerSlot).
func attachPartyStats(members []PartySlot, stats map[string]riot.PlayerStats) {
	for i := range members {
		st, ok := stats[members[i].PUUID]
		if !ok {
			continue
		}
		cp := st
		members[i].Stats = &cp
		if members[i].Name == "" {
			members[i].Name = st.Name
		}
		if members[i].Tier == 0 {
			members[i].Tier = st.Tier
		}
	}
}

// lobbyStats returns cached tracker rows for the given match, kicking off a
// background fetch for any players not cached yet. The roster grows across the
// same match id — agent select shows only the 5 allies, but core-game shows all
// 10 — so we fetch the *missing* puuids and merge, rather than fetching once and
// never again (which left the enemy team stuck on "Loading"). It returns
// whatever is cached right now so the poll never blocks; the next poll (~1 Hz)
// picks up the filled cache and re-emits. Caller need not hold s.mu.
func (s *riotSource) lobbyStats(c *riot.Client, matchID, queue string, puuids []string) map[string]riot.PlayerStats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	if matchID != s.statsKey {
		// New match → drop the old cache and (below) refetch.
		s.statsKey, s.statsByPUUID, s.statsFetching = matchID, nil, false
	}

	// Players we don't have a row for yet (the enemy team appears only once the
	// match goes live). Fetch just these so allies cached in agent select stay.
	missing := missingPUUIDs(s.statsByPUUID, puuids)
	if matchID != "" && len(missing) > 0 && !s.statsFetching {
		s.statsFetching = true
		key := matchID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			res := c.LobbyStats(ctx, missing, queue, recentMatchesN)
			s.statsMu.Lock()
			merged := s.statsKey == key && len(res) > 0
			if merged { // ignore if the match changed mid-fetch
				if s.statsByPUUID == nil {
					s.statsByPUUID = make(map[string]riot.PlayerStats, len(res))
				}
				for k, v := range res {
					s.statsByPUUID[k] = v
				}
			}
			s.statsFetching = false
			s.statsMu.Unlock()
			// Re-emit so the new rows reach the phone now, not on the next tick.
			if merged && s.onUpdate != nil {
				s.onUpdate()
			}
		}()
	}

	out := make(map[string]riot.PlayerStats, len(s.statsByPUUID))
	for k, v := range s.statsByPUUID {
		out[k] = v
	}
	return out
}

// missingPUUIDs returns the requested puuids that aren't in the cache yet, so a
// growing roster (allies → both teams) only fetches the newcomers. A player who
// fetched but came back sparse still has a row, so they aren't re-requested.
func missingPUUIDs(have map[string]riot.PlayerStats, want []string) []string {
	var miss []string
	for _, p := range want {
		if p == "" {
			continue
		}
		if _, ok := have[p]; !ok {
			miss = append(miss, p)
		}
	}
	return miss
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

// partyGroups returns the inferred premade group per puuid for the current
// roster, kicking off a one-shot background detection when the roster changes.
// Like lobbyStats it never blocks the poll: it returns whatever is computed now
// and fires onUpdate when a fresh result lands. Always all-queues — a party is a
// party regardless of playlist. Caller need not hold s.mu.
func (s *riotSource) partyGroups(c *riot.Client, matchID string, players []PlayerSlot) map[string]int {
	sig := rosterSig(matchID, players)

	s.partyMu.Lock()
	defer s.partyMu.Unlock()

	if sig != s.partySig {
		s.partySig, s.partyByPUUID, s.partyFetching = sig, nil, false
	}

	if matchID != "" && s.partyByPUUID == nil && !s.partyFetching && len(players) >= 2 {
		s.partyFetching = true
		key := sig
		team := make(map[string]string, len(players))
		for _, p := range players {
			if p.PUUID != "" {
				team[p.PUUID] = p.Team
			}
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			groups := c.DetectParties(ctx, team, "", partyMatchesN)
			byPUUID := make(map[string]int, len(team))
			for i, g := range groups {
				for _, puuid := range g {
					byPUUID[puuid] = i + 1
				}
			}
			s.partyMu.Lock()
			fresh := s.partySig == key
			if fresh {
				s.partyByPUUID = byPUUID // non-nil (maybe empty) → "computed; don't refetch"
			}
			s.partyFetching = false
			s.partyMu.Unlock()
			if fresh && s.onUpdate != nil {
				s.onUpdate() // re-emit so the borders appear without waiting for a tick
			}
		}()
	}

	out := make(map[string]int, len(s.partyByPUUID))
	for k, v := range s.partyByPUUID {
		out[k] = v
	}
	return out
}

// attachPartyGroups stamps each player's inferred group id (0 = solo/none).
func attachPartyGroups(players []PlayerSlot, groups map[string]int) {
	if len(groups) == 0 {
		return
	}
	for i := range players {
		players[i].PartyGroup = groups[players[i].PUUID]
	}
}

// rosterSig is a stable key for a match's roster (match id + sorted puuids) so
// party detection recomputes when the set of players changes — pregame's 5 allies
// to core-game's 10 — but not on every poll.
func rosterSig(matchID string, players []PlayerSlot) string {
	ids := make([]string, 0, len(players))
	for _, p := range players {
		if p.PUUID != "" {
			ids = append(ids, p.PUUID)
		}
	}
	sort.Strings(ids)
	return matchID + "#" + strings.Join(ids, ",")
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

// withClient runs fn against the live client (or errors if not authenticated),
// the shared shape for the party actions below (mirrors Select/Lock locking).
func (s *riotSource) withClient(fn func(*riot.Client) error) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return errors.New("not authenticated")
	}
	return fn(c)
}

func (s *riotSource) GenerateInviteCode(ctx context.Context, id string) error {
	return s.withClient(func(c *riot.Client) error { return c.GenerateInviteCode(ctx, id) })
}
func (s *riotSource) DisableInviteCode(ctx context.Context, id string) error {
	return s.withClient(func(c *riot.Client) error { return c.DisableInviteCode(ctx, id) })
}
func (s *riotSource) JoinByCode(ctx context.Context, code string) error {
	return s.withClient(func(c *riot.Client) error { return c.JoinByCode(ctx, code) })
}
func (s *riotSource) LeaveParty(ctx context.Context) error {
	return s.withClient(func(c *riot.Client) error { return c.LeaveParty(ctx) })
}
func (s *riotSource) KickMember(ctx context.Context, puuid string) error {
	return s.withClient(func(c *riot.Client) error { return c.KickMember(ctx, puuid) })
}
func (s *riotSource) SetAccessibility(ctx context.Context, id string, open bool) error {
	return s.withClient(func(c *riot.Client) error { return c.SetAccessibility(ctx, id, open) })
}
func (s *riotSource) ChangeQueue(ctx context.Context, id, queueID string) error {
	return s.withClient(func(c *riot.Client) error { return c.ChangeQueue(ctx, id, queueID) })
}
func (s *riotSource) StartMatchmaking(ctx context.Context, id string) error {
	return s.withClient(func(c *riot.Client) error { return c.StartMatchmaking(ctx, id) })
}
func (s *riotSource) StopMatchmaking(ctx context.Context, id string) error {
	return s.withClient(func(c *riot.Client) error { return c.StopMatchmaking(ctx, id) })
}
