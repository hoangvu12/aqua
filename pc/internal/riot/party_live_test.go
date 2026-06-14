package riot

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveDetectParties infers premades in your *current* live match the
// tracker.gg way (shared recent-match partyId) and prints the result so we can
// eyeball accuracy. Runs only with AQUA_LIVE_PARTY=1 and VALORANT in a match.
//
//	AQUA_LIVE_PARTY=1 go -C pc test ./internal/riot -run TestLiveDetectParties -v -count=1
//	AQUA_PARTY_N=25 controls how many recent matches per player to scan (default 25).
func TestLiveDetectParties(t *testing.T) {
	if os.Getenv("AQUA_LIVE_PARTY") != "1" {
		t.Skip("set AQUA_LIVE_PARTY=1 with VALORANT in a match to infer parties")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := Authenticate(ctx)
	if err != nil {
		t.Skipf("authenticate (Riot Client running?): %v", err)
	}

	// Current roster + team. Prefer coregame (both teams); fall back to pregame.
	team := map[string]string{}
	queue := "competitive"
	if mid, err := c.CoreGamePlayer(ctx); err == nil && mid != "" {
		seats, _ := c.CoreGameMatch(ctx, mid)
		for _, s := range seats {
			team[s.Subject] = s.TeamID
		}
		t.Logf("coregame %s → %d players", mid, len(seats))
	} else if mid, err := c.PregamePlayer(ctx); err == nil && mid != "" {
		if m, err := c.PregameMatch(ctx, mid); err == nil {
			for _, p := range m.AllyTeam.Players {
				team[p.Subject] = "Blue"
			}
		}
		t.Logf("pregame %s → %d allies (ally team only)", mid, len(team))
	}
	if len(team) < 2 {
		t.Skip("not in a live match with a roster")
	}

	n := 25
	if s := os.Getenv("AQUA_PARTY_N"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}

	start := time.Now()
	groups := c.DetectParties(ctx, team, queue, n)
	elapsed := time.Since(start).Round(time.Millisecond)

	// Resolve names for readable output.
	puuids := make([]string, 0, len(team))
	for p := range team {
		puuids = append(puuids, p)
	}
	names, _ := c.Names(ctx, puuids)
	label := func(p string) string {
		if n := names[p]; n != "" {
			return n
		}
		return p[:8]
	}

	t.Logf("scanned last %d %s matches/player in %s → %d inferred parties", n, queue, elapsed, len(groups))
	if len(groups) == 0 {
		t.Logf("(no premades detected — all solos, or none have completed a match together recently)")
	}
	for i, g := range groups {
		who := make([]string, len(g))
		for j, p := range g {
			who[j] = label(p) + " [" + team[p] + "]"
		}
		t.Logf("  party %d: %s", i+1, strings.Join(who, "  +  "))
	}
}
