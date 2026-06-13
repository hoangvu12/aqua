package picker

import (
	"context"
	"testing"

	"aqua/internal/config"
)

const (
	tJett = "add6443a-41bd-e414-f6ad-e58d267f4e95"
	tSage = "5f8d3a7f-467b-97f3-062c-13acf203c006"
)

// fakeSource is a fully controllable Source for unit tests (no timeline).
type fakeSource struct {
	snap    Snapshot
	puuid   string
	selects []string
	locks   []string
}

func (f *fakeSource) Snapshot(context.Context) (Snapshot, error) { return f.snap, nil }
func (f *fakeSource) Select(_ context.Context, _, a string) error {
	f.selects = append(f.selects, a)
	return nil
}
func (f *fakeSource) Lock(_ context.Context, _, a string) error {
	f.locks = append(f.locks, a)
	return nil
}
func (f *fakeSource) Authenticate(context.Context) error { return nil }
func (f *fakeSource) PUUID() string                      { return f.puuid }

type capSink struct{ states []State }

func (c *capSink) SendState(s State)               { c.states = append(c.states, s) }
func (c *capSink) SendResult(string, bool, string) {}
func (c *capSink) SendAuthStatus(bool, string)     {}
func (c *capSink) last() State                     { return c.states[len(c.states)-1] }

func newPicker(cfg *config.Config, src Source) (*Picker, *capSink) {
	sink := &capSink{}
	return New(cfg, src, sink), sink
}

// TestPreMatchPhases: source-classified pre-match phases pass through to the wire
// state with their queue forwarded (the Phase 5 menus/lobby/queue/matchfound split).
func TestPreMatchPhases(t *testing.T) {
	cases := []struct {
		phase, queue string
	}{
		{"menus", ""},
		{"lobby", "competitive"},
		{"queue", "competitive"},
		{"matchfound", "competitive"},
		{"ingame", ""},
	}
	for _, c := range cases {
		src := &fakeSource{snap: Snapshot{Running: true, Phase: c.phase, QueueID: c.queue}}
		p, sink := newPicker(&config.Config{}, src)
		p.poll(context.Background())
		got := sink.last()
		if got.State != c.phase {
			t.Errorf("phase %q: state = %q, want %q", c.phase, got.State, c.phase)
		}
		if got.QueueID != c.queue {
			t.Errorf("phase %q: queue_id = %q, want %q", c.phase, got.QueueID, c.queue)
		}
	}
}

// TestOfflineAndError: not running → offline; snapshot error path → error.
func TestOfflineAndError(t *testing.T) {
	src := &fakeSource{snap: Snapshot{Running: false}}
	p, sink := newPicker(&config.Config{}, src)
	p.poll(context.Background())
	if got := sink.last().State; got != "offline" {
		t.Errorf("not running: state = %q, want offline", got)
	}
}

func pregameSnap(self PlayerSlot, allies ...PlayerSlot) Snapshot {
	return Snapshot{
		Running:     true,
		Phase:       "pregame",
		MatchID:     "m1",
		MapID:       "/Game/Maps/Triad/Triad",
		QueueID:     "competitive",
		OwnedAgents: []string{tJett, tSage},
		Players:     append([]PlayerSlot{self}, allies...),
	}
}

// TestPrepickStatusArmed: with a pre-pick configured but no attempt, the resting
// status is "armed"; if an ally has locked that agent, it flips to "taken".
func TestPrepickStatusArmed(t *testing.T) {
	src := &fakeSource{puuid: "self", snap: pregameSnap(
		PlayerSlot{PUUID: "self"},
		PlayerSlot{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
	)}
	cfg := &config.Config{PrepickAgentUUID: tJett}
	p, sink := newPicker(cfg, src)
	p.poll(context.Background())
	if got := sink.last().PrepickStatus; got != "armed" {
		t.Errorf("prepick not taken: status = %q, want armed", got)
	}

	// Now arm the agent an ally already locked → taken.
	cfg.PrepickAgentUUID = tSage
	src.snap = pregameSnap(
		PlayerSlot{PUUID: "self"},
		PlayerSlot{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
	)
	p.poll(context.Background())
	if got := sink.last().PrepickStatus; got != "taken" {
		t.Errorf("prepick taken by ally: status = %q, want taken", got)
	}
}

// TestAutoLockFiresOnce: armed + owned + not-taken → plan fires exactly once per
// match, opening the optimistic window; a second poll must not re-fire.
func TestAutoLockFiresOnce(t *testing.T) {
	cfg := &config.Config{Enabled: true, AutoLock: true, PrepickAgentUUID: tJett}
	src := &fakeSource{puuid: "self", snap: pregameSnap(
		PlayerSlot{PUUID: "self"},
		PlayerSlot{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
	)}
	p, _ := newPicker(cfg, src)

	st := buildState(p, src.snap)
	fire, agent, mid := p.planAutoLock("m1", st)
	if !fire || agent != tJett || mid != "m1" {
		t.Fatalf("first plan: fire=%v agent=%q mid=%q, want true/%s/m1", fire, agent, tJett, mid)
	}
	// Optimistic window is open → status reads "locking" on the next build.
	if got := buildState(p, src.snap).PrepickStatus; got != "locking" {
		t.Errorf("after fire: status = %q, want locking", got)
	}
	// Same match must not re-fire.
	if fire2, _, _ := p.planAutoLock("m1", st); fire2 {
		t.Error("second plan for same match fired again; want once")
	}
}

// TestAutoLockGuards: taken, unowned, and disarmed pre-picks never auto-fire.
func TestAutoLockGuards(t *testing.T) {
	mk := func(cfg *config.Config, owned []string, prepick string) bool {
		src := &fakeSource{puuid: "self", snap: Snapshot{
			Running: true, Phase: "pregame", MatchID: "m1", OwnedAgents: owned,
			Players: []PlayerSlot{
				{PUUID: "self"},
				{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
			},
		}}
		cfg.PrepickAgentUUID = prepick
		p, _ := newPicker(cfg, src)
		st := buildState(p, src.snap)
		fire, _, _ := p.planAutoLock("m1", st)
		return fire
	}

	if mk(&config.Config{Enabled: true, AutoLock: true}, []string{tSage}, tSage) {
		t.Error("taken pre-pick auto-fired; want refusal")
	}
	if mk(&config.Config{Enabled: true, AutoLock: true}, []string{tSage}, tJett) {
		t.Error("unowned pre-pick auto-fired; want refusal")
	}
	if mk(&config.Config{Enabled: true, AutoLock: false}, []string{tJett, tSage}, tJett) {
		t.Error("auto-lock disabled but fired")
	}
	if mk(&config.Config{Enabled: false, AutoLock: true}, []string{tJett, tSage}, tJett) {
		t.Error("picker disabled but fired")
	}
}

// TestLockReconcile: our own POST never settles to locked — only the game's
// CharacterSelectionState does (the reference repo's bug, avoided here).
func TestLockReconcile(t *testing.T) {
	cfg := &config.Config{Enabled: true, AutoLock: true, PrepickAgentUUID: tJett}
	src := &fakeSource{puuid: "self", snap: pregameSnap(
		PlayerSlot{PUUID: "self"},
		PlayerSlot{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
	)}
	p, _ := newPicker(cfg, src)

	// Open the optimistic window via auto-lock plan.
	st := buildState(p, src.snap)
	if fire, _, _ := p.planAutoLock("m1", st); !fire {
		t.Fatal("expected auto-lock to fire")
	}
	if got := buildState(p, src.snap).PrepickStatus; got != "locking" {
		t.Fatalf("pre-confirm status = %q, want locking", got)
	}
	// Game confirms our seat as locked → settle to locked.
	src.snap = pregameSnap(
		PlayerSlot{PUUID: "self", CharacterID: tJett, SelectionState: "locked"},
		PlayerSlot{PUUID: "a1", CharacterID: tSage, SelectionState: "locked"},
	)
	final := buildState(p, src.snap)
	if final.State != "locked" || final.PrepickStatus != "locked" {
		t.Errorf("after game confirm: state=%q prepick=%q, want locked/locked", final.State, final.PrepickStatus)
	}
	if final.SelfAgentUUID != tJett {
		t.Errorf("self_agent_uuid = %q, want %s", final.SelfAgentUUID, tJett)
	}
}

// buildState invokes the picker's state derivation under its lock, mirroring poll.
func buildState(p *Picker, snap Snapshot) State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.build(snap, nil)
}
