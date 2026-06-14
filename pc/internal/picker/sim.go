package picker

import (
	"context"
	"sync"

	"aqua/internal/riot"
)

// Simulation agent UUIDs (real valorant-api UUIDs so the phone renders art).
const (
	simSelfPUUID = "sim-self"
	simTakenUUID = "5f8d3a7f-467b-97f3-062c-13acf203c006" // an ally-locked agent
	simJettUUID  = "add6443a-41bd-e414-f6ad-e58d267f4e95" // owned; pre-pick target
)

// simStat fabricates a tracker row. Tier numbers follow valorant-api
// competitivetiers (Iron 1 = 3 … Radiant = 27); recent is newest-first W/L,
// turned into RecentMatch with a synthesized RR delta per result (sim is all
// competitive) so the phone exercises the RR-folded streak.
func simStat(name string, tier, peak int, kd, adr, hs, win float64, recent []bool) *riot.PlayerStats {
	wins := 0
	rm := make([]riot.RecentMatch, len(recent))
	for i, w := range recent {
		rr := -(17 + i%3) // a plausible loss: -17..-19
		if w {
			wins++
			rr = 19 + i%3 // a plausible win: +19..+21
		}
		v := rr
		rm[i] = riot.RecentMatch{Won: w, RR: &v}
	}
	return &riot.PlayerStats{
		PUUID: "sim-" + name, Name: name, Tier: tier, RR: 47, PeakTier: peak,
		Matches: len(recent), Wins: wins, WinPct: win, KD: kd, ADR: adr, HSPct: hs, Recent: rm,
	}
}

// Real valorant-api agent UUIDs so every -sim scoreboard portrait renders.
const (
	simSovaUUID      = "320b2a48-4d9b-a075-30f1-1f93a9b638fa"
	simBrimstoneUUID = "9f0d8ba9-4140-b941-57d3-a7ad57c6b417"
	simOmenUUID      = "8e253930-4c05-31dd-1b6c-968525494517"
	simKilljoyUUID   = "1e58de9c-4950-5125-93e9-a0aee9f98746"
	simCypherUUID    = "117ed9e3-49f3-6512-3ccf-0cada7e3823b"
	simReynaUUID     = "a3bfb853-43b2-7238-a4f1-ad90e9e46bce"
	simRazeUUID      = "f94c3b30-42be-e959-889c-5aa313dba261"
)

// Real valorant-api skin renders so the -sim scoreboard exercises the equipped-
// skins strip (media host; the phone rewrites it to /cdn in production).
const simMedia = "https://media.valorant-api.com/weaponskins/"

var simSkinsSelf = []SeatSkin{
	{Weapon: "Vandal", Name: "Prelude to Chaos Vandal", Image: simMedia + "522a264e-4ca7-adb0-6cf1-28b2ef938727/displayicon.png"},
	{Weapon: "Operator", Name: "RGX 11z Pro Operator", Image: simMedia + "2e1936ed-4582-628f-da9c-25a7f47323cc/displayicon.png"},
	{Weapon: "Knife", Name: "Reaver Karambit", Image: simMedia + "b73d7b16-4652-bc5b-5c4c-068aabb19d0a/displayicon.png"},
}

var simSkinsAlly = []SeatSkin{
	{Weapon: "Phantom", Name: "Recon Phantom", Image: simMedia + "d67b929f-4431-61c0-286e-3ebf3d11c4af/displayicon.png"},
	{Weapon: "Vandal", Name: "Primordium Vandal", Image: simMedia + "a70fd508-44ea-8de3-3b30-d3a7eb9db42e/displayicon.png"},
}

// simScoreboard is a fixed 10-player live match (5 ally + 5 enemy) for -sim, so
// the in-match scoreboard renders without a real game. Self is first.
func simScoreboard(selfAgent string) []PlayerSlot {
	return []PlayerSlot{
		{PUUID: simSelfPUUID, CharacterID: selfAgent, Team: "ally", Name: "You", Skins: simSkinsSelf,
			Stats: simStat("You", 19, 24, 1.21, 158, 28.0, 52, []bool{true, false, true, true, false})},
		{PUUID: "sim-a1", CharacterID: simReynaUUID, Team: "ally", Name: "wazuu#1406", Skins: simSkinsAlly,
			Stats: simStat("wazuu#1406", 19, 20, 1.46, 194, 22.0, 45, []bool{true, true, false, true, true})},
		{PUUID: "sim-a2", CharacterID: simBrimstoneUUID, Team: "ally", Name: "BrimstonMimstone#NA1",
			Stats: simStat("BrimstonMimstone#NA1", 14, 16, 0.57, 96, 9.1, 36, []bool{false, false, true, false, false})},
		{PUUID: "sim-a3", CharacterID: simSovaUUID, Team: "ally", Name: "PostBTW#EUW",
			Stats: simStat("PostBTW#EUW", 10, 12, 0.80, 144, 33.3, 0, []bool{false, false})},
		{PUUID: "sim-a4", CharacterID: simJettUUID, Team: "ally", Name: "penna#777",
			Stats: simStat("penna#777", 6, 9, 0.25, 68, 12.5, 0, []bool{false, false})},

		{PUUID: "sim-e1", CharacterID: simTakenUUID, Team: "enemy", Name: "ErSupremoLaziale#EU",
			Stats: simStat("ErSupremoLaziale#EU", 15, 20, 2.18, 207, 23.2, 100, []bool{true, true, true})},
		{PUUID: "sim-e2", CharacterID: simOmenUUID, Team: "enemy", Name: "Sykkuno#0001",
			Stats: simStat("Sykkuno#0001", 21, 22, 1.33, 172, 19.4, 58, []bool{true, false, true, true, true})},
		{PUUID: "sim-e3", CharacterID: simKilljoyUUID, Team: "enemy", Name: "miyu#vn2",
			Stats: simStat("miyu#vn2", 17, 18, 0.94, 131, 15.0, 40, []bool{false, true, false, false, true})},
		{PUUID: "sim-e4", CharacterID: simRazeUUID, Team: "enemy", Name: "Tenz#TENZ",
			Stats: simStat("Tenz#TENZ", 27, 27, 1.88, 221, 31.7, 70, []bool{true, true, false, true, true})},
		{PUUID: "sim-e5", CharacterID: simCypherUUID, Team: "enemy", Name: "noob#123",
			Stats: simStat("noob#123", 0, 0, 0.61, 88, 11.2, 25, []bool{false, false, true, false})},
	}
}

// simSource fakes the game so the full flow can be exercised with no live match
// (plan §Testing). Timeline by poll tick (~1 Hz): walks the whole pre-match
// ladder — menus → lobby → queue → matchfound — then agent select (one ally
// already locked). Each pre-match step lasts 2 ticks so a phone that connects
// mid-stream still observes every state. After we (or auto-lock) lock, a few
// ticks later → ingame.
type simSource struct {
	mu       sync.Mutex
	tick     int
	ourAgent string
	ourState string // ""|selected|locked
	lockedAt int

	// Party (lobby) sim state, mutated by the party actions so the phone's drawer
	// reflects taps. Self is the owner so every owner-only control is exercisable.
	inviteCode string
	closed     bool
	queue      string
}

// NewSimSource returns a scripted game source for testing.
func NewSimSource() Source { return &simSource{} }

func (s *simSource) PUUID() string { return simSelfPUUID }

func (s *simSource) Authenticate(context.Context) error { return nil }

func (s *simSource) Snapshot(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick++

	owned := []string{simTakenUUID, simJettUUID, s.ourAgent}

	// A fake party (self = owner + one ally) carried across every pre-match rung,
	// so the lobby drawer renders and its actions have something to mutate.
	q := s.queue
	if q == "" {
		q = "competitive"
	}
	acc := "OPEN"
	if s.closed {
		acc = "CLOSED"
	}
	party := func(phase string) Snapshot {
		queue := q
		if phase == "menus" {
			queue = ""
		}
		return Snapshot{
			Running: true, Phase: phase, QueueID: queue, Locale: "vi-VN", OwnedAgents: owned,
			PartyID: "sim-party", IsOwner: true, Accessibility: acc, InviteCode: s.inviteCode, MaxPartySize: 5,
			PartyMembers: []PartySlot{
				{PUUID: simSelfPUUID, Name: "You", IsOwner: true, Self: true, Tier: 19,
					Stats: simStat("You", 19, 24, 1.21, 158, 28.0, 52, []bool{true, false, true, true, false})},
				{PUUID: "sim-ally-1", Name: "wazuu#1406", Tier: 19,
					Stats: simStat("wazuu#1406", 19, 20, 1.46, 194, 22.0, 45, []bool{true, true, false, true, true})},
			},
		}
	}

	// Pre-match ladder (2 ticks per rung) before agent select opens.
	switch {
	case s.tick <= 2:
		return party("menus"), nil
	case s.tick <= 4:
		return party("lobby"), nil
	case s.tick <= 6:
		return party("queue"), nil
	case s.tick <= 8:
		return party("matchfound"), nil
	}

	if s.ourState == "locked" && s.tick > s.lockedAt+3 {
		return Snapshot{
			Running: true, Phase: "ingame", MatchID: "sim-match", Locale: "vi-VN",
			OwnedAgents: owned, Players: simScoreboard(s.ourAgent),
			ScoreAlly: 7, ScoreEnemy: 5, HasScore: true,
		}, nil
	}
	return Snapshot{
		Running:              true,
		Phase:                "pregame",
		MatchID:              "sim-match",
		MapID:                "/Game/Maps/Triad/Triad", // Haven
		QueueID:              "competitive",
		PhaseTimeRemainingNS: 45_000_000_000,
		Locale:               "vi-VN",
		OwnedAgents:          owned,
		Players: []PlayerSlot{
			{PUUID: simSelfPUUID, CharacterID: s.ourAgent, SelectionState: s.ourState, Name: "You", Team: "ally",
				Stats: simStat("You", 19, 24, 1.21, 158, 28.0, 52, []bool{true, false, true, true, false})},
			{PUUID: "sim-ally-1", CharacterID: simTakenUUID, SelectionState: "locked", Name: "wazuu#1406", Team: "ally",
				Stats: simStat("wazuu#1406", 19, 20, 1.46, 194, 22.0, 45, []bool{true, true, false, true, true})},
			{PUUID: "sim-ally-2", CharacterID: "", SelectionState: "", Name: "BrimstonMimstone#NA1", Team: "ally",
				Stats: simStat("BrimstonMimstone#NA1", 14, 16, 0.57, 96, 9.1, 36, []bool{false, false, true, false, false})},
		},
	}, nil
}

func (s *simSource) Select(_ context.Context, _, agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ourAgent, s.ourState = agentID, "selected"
	return nil
}

func (s *simSource) Lock(_ context.Context, _, agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ourAgent, s.ourState, s.lockedAt = agentID, "locked", s.tick
	return nil
}

// Quit (dodge) drops us out of agent select. The sim models this by rewinding
// the timeline to the pre-match menus, clearing our pick — exactly what a real
// dodge does (back to the lobby, minus the penalty the sim doesn't fake).
func (s *simSource) Quit(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick, s.ourAgent, s.ourState, s.lockedAt = 0, "", "", 0
	return nil
}

// ---- party actions (mutate sim state so the drawer reflects taps) ----------

func (s *simSource) GenerateInviteCode(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inviteCode = "SIM42"
	return nil
}
func (s *simSource) DisableInviteCode(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inviteCode = ""
	return nil
}
func (s *simSource) SetAccessibility(_ context.Context, _ string, open bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = !open
	return nil
}
func (s *simSource) ChangeQueue(_ context.Context, _, queueID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = queueID
	return nil
}
func (s *simSource) JoinByCode(context.Context, string) error       { return nil }
func (s *simSource) LeaveParty(context.Context) error               { return nil }
func (s *simSource) KickMember(context.Context, string) error       { return nil }
func (s *simSource) StartMatchmaking(context.Context, string) error { return nil }
func (s *simSource) StopMatchmaking(context.Context, string) error  { return nil }
