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
	queue := "" // all queues — a party is a party regardless of playlist
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

	puuids := make([]string, 0, len(team))
	for p := range team {
		puuids = append(puuids, p)
	}
	names, _ := c.Names(ctx, puuids)
	label := func(p string) string {
		if nm := names[p]; nm != "" {
			return nm
		}
		return p[:8]
	}

	start := time.Now()

	// Inline the DetectParties steps so one pass shows the full picture with the
	// minimum number of PD calls: exactly one history fetch per player. (0 history
	// across the board almost always means we got rate-limited — back off and retry.)
	hist := make([][]string, len(puuids))
	runBounded(ctx, len(puuids), statsConcurrency, func(i int) {
		hist[i], _ = c.MatchHistory(ctx, puuids[i], queue, n)
	})
	total := 0
	for i, p := range puuids {
		self := ""
		if p == c.PUUID() {
			self = " (self)"
		}
		t.Logf("  history %-22s%s %d", label(p), self, len(hist[i]))
		total += len(hist[i])
	}

	count := map[string]int{}
	for _, ids := range hist {
		seen := map[string]bool{}
		for _, id := range ids {
			if id != "" && !seen[id] {
				seen[id] = true
				count[id]++
			}
		}
	}
	shared := []string{}
	for id, ct := range count {
		if ct >= 2 {
			shared = append(shared, id)
		}
	}
	past := make([]*MatchDetail, 0, len(shared))
	for _, id := range shared {
		md, err := c.MatchDetails(ctx, id)
		if err != nil {
			t.Logf("  shared match %s: fetch error: %v", id[:8], err)
			continue
		}
		past = append(past, md)
		// Show the current-match players in this shared match: their historical
		// party + team. Same partyId on the same current team → a detected premade.
		t.Logf("  shared match %s:", id[:8])
		for _, p := range puuids {
			if mp, ok := md.Players[p]; ok {
				pid := mp.PartyID
				if len(pid) > 8 {
					pid = pid[:8]
				}
				t.Logf("      %-22s pastParty=%-8s pastTeam=%-4s nowTeam=%s", label(p), pid, mp.Team, team[p])
			}
		}
	}
	groups := InferParties(team, past)
	elapsed := time.Since(start).Round(time.Millisecond)

	t.Logf("history rows: %d | matches shared by ≥2 players: %d | scanned last %d (all queues)",
		total, len(shared), n)
	if total == 0 {
		t.Fatalf("0 history rows for everyone — almost certainly rate-limited; wait ~1 min and retry")
	}
	t.Logf("→ %d inferred parties in %s", len(groups), elapsed)
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
