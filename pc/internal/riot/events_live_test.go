package riot

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestLiveEventStream connects to the *running* Riot Client's local websocket and
// captures events for a few seconds. It only runs when AQUA_LIVE_WS=1 and the
// Riot Client is up (lockfile present) — otherwise it skips, so it never breaks
// CI. Use it to verify the protocol against the current client and to see the
// real resource URIs (notably the chat presence version) the live client emits.
//
//	AQUA_LIVE_WS=1 go -C pc test ./internal/riot -run TestLiveEventStream -v -count=1
func TestLiveEventStream(t *testing.T) {
	if os.Getenv("AQUA_LIVE_WS") != "1" {
		t.Skip("set AQUA_LIVE_WS=1 with VALORANT running to exercise the live websocket")
	}
	if _, err := ReadLockfile(); err != nil {
		t.Skipf("Riot Client not running: %v", err)
	}

	window := 8 * time.Second
	if s := os.Getenv("AQUA_LIVE_WS_SECONDS"); s != "" {
		if n, err := time.ParseDuration(s + "s"); err == nil {
			window = n
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()

	var (
		mu       sync.Mutex
		count    int
		relevant int
		uris     = map[string]int{}
	)
	s := &EventStream{OnEvent: func(ev Event) {
		mu.Lock()
		count++
		uris[ev.URI]++
		if IsMatchRelevant(ev.URI) {
			relevant++
		}
		mu.Unlock()
	}}

	// session blocks reading events until ctx times out (the success path) or it
	// can't connect/subscribe (returns early). We diagnose via elapsed + count.
	start := time.Now()
	err := s.session(ctx)
	elapsed := time.Since(start)

	mu.Lock()
	defer mu.Unlock()

	// A failed dial/subscribe returns well before the window elapses with nothing.
	if elapsed < window-2*time.Second && count == 0 {
		t.Fatalf("session ended early after %s with no events — dial/subscribe failed: %v",
			elapsed.Round(time.Millisecond), err)
	}

	t.Logf("captured %d events (%d match-relevant) over %s across %d distinct URIs (* = triggers refresh):",
		count, relevant, elapsed.Round(time.Millisecond), len(uris))

	type uc struct {
		uri string
		n   int
	}
	list := make([]uc, 0, len(uris))
	for u, n := range uris {
		list = append(list, uc{u, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, e := range list {
		tag := " "
		if IsMatchRelevant(e.uri) {
			tag = "*"
		}
		t.Logf("  %s %4d  %s", tag, e.n, e.uri)
	}

	if count == 0 {
		t.Errorf("connected but received 0 events in %s — expected at least session heartbeats in-client", window)
	}
	if relevant == 0 {
		// Not a failure: match-relevant events (presence/messaging) fire on state
		// transitions, so a quiet in-match window legitimately sees none. Capture
		// across a round end / queue / agent select to see them.
		t.Logf("note: 0 match-relevant events this window (expected only on transitions)")
	}
}
