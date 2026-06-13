package picker

import (
	"context"
	"sync"
)

// Simulation agent UUIDs (real valorant-api UUIDs so the phone renders art).
const (
	simSelfPUUID = "sim-self"
	simTakenUUID = "5f8d3a7f-467b-97f3-062c-13acf203c006" // an ally-locked agent
	simJettUUID  = "add6443a-41bd-e414-f6ad-e58d267f4e95" // owned; pre-pick target
)

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

	// Pre-match ladder (2 ticks per rung) before agent select opens.
	switch {
	case s.tick <= 2:
		return Snapshot{Running: true, Phase: "menus", Locale: "vi-VN", OwnedAgents: owned}, nil
	case s.tick <= 4:
		return Snapshot{Running: true, Phase: "lobby", QueueID: "competitive", Locale: "vi-VN", OwnedAgents: owned}, nil
	case s.tick <= 6:
		return Snapshot{Running: true, Phase: "queue", QueueID: "competitive", Locale: "vi-VN", OwnedAgents: owned}, nil
	case s.tick <= 8:
		return Snapshot{Running: true, Phase: "matchfound", QueueID: "competitive", Locale: "vi-VN", OwnedAgents: owned}, nil
	}

	if s.ourState == "locked" && s.tick > s.lockedAt+3 {
		return Snapshot{Running: true, Phase: "ingame", Locale: "vi-VN", OwnedAgents: owned}, nil
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
			{PUUID: simSelfPUUID, CharacterID: s.ourAgent, SelectionState: s.ourState, Name: "You"},
			{PUUID: "sim-ally-1", CharacterID: simTakenUUID, SelectionState: "locked", Name: "Ally1"},
			{PUUID: "sim-ally-2", CharacterID: "", SelectionState: "", Name: "Ally2"},
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
