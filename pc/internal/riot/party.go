package riot

// Live premade ("party") detection, the tracker.gg way. Riot strips partyId from
// the live pregame/coregame endpoints, so a party can't be read directly during a
// match. Instead we infer it: two players in the current match are likely premade
// if they've shared a party (same partyId) in a *recent completed* match. This is
// a heuristic — it can't see a brand-new party that has never finished a game
// together, and a since-broken-up duo can read as a false positive — exactly the
// limits tracker.gg states ("very likely, but not guaranteed").
//
// Efficiency: a past match two current players shared appears in BOTH their match
// histories, so we only need match-details for match IDs seen in ≥2 current
// players' histories. For a lobby of randoms that's almost nothing; only real
// premades surface matches worth fetching.

import (
	"context"
	"sort"
	"sync"
)

// InferParties groups current-match players into premades. team maps each
// player's puuid → current team ("Blue"/"Red"); past is the details of recent
// matches shared by ≥2 of those players. Two players are linked when they shared
// a partyId in some past match AND are on the same team now (a party is always
// one team). Returns one sorted slice of puuids per group of size ≥2; solos are
// omitted. Pure (no I/O) so it's unit-testable and reusable.
func InferParties(team map[string]string, past []*MatchDetail) [][]string {
	// Union-find over the current-match puuids.
	parent := make(map[string]string, len(team))
	for p := range team {
		parent[p] = p
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]] // path-halving
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) { parent[find(a)] = find(b) }

	for _, md := range past {
		if md == nil {
			continue
		}
		// Bucket the current players present in this past match by that match's
		// partyId, ignoring solos (a unique partyId per player).
		byParty := map[string][]string{}
		for puuid := range team {
			mp, ok := md.Players[puuid]
			if !ok || mp.PartyID == "" {
				continue
			}
			byParty[mp.PartyID] = append(byParty[mp.PartyID], puuid)
		}
		for _, members := range byParty {
			for i := 0; i < len(members); i++ {
				for j := i + 1; j < len(members); j++ {
					if team[members[i]] == team[members[j]] {
						union(members[i], members[j])
					}
				}
			}
		}
	}

	groups := map[string][]string{}
	for p := range team {
		r := find(p)
		groups[r] = append(groups[r], p)
	}
	out := [][]string{}
	for _, g := range groups {
		if len(g) >= 2 {
			sort.Strings(g)
			out = append(out, g)
		}
	}
	// Stable order (largest first, then lexicographic) for deterministic output.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// DetectParties infers the premades in a live match. team maps puuid → current
// team; queue filters history ("competitive" is the sensible default); n is how
// many recent matches to scan per player (tracker.gg uses 25). It fetches each
// player's history, keeps only match IDs shared by ≥2 players (the only ones that
// can reveal co-occurrence), fetches just those details, and runs InferParties.
// Best-effort: a player or match that fails to load is simply skipped.
//
// Still PD-rate-limited — call it once per match and cache; never from a loop.
func (c *Client) DetectParties(ctx context.Context, team map[string]string, queue string, n int) [][]string {
	if len(team) < 2 {
		return [][]string{}
	}
	if n <= 0 {
		n = 25
	}
	puuids := make([]string, 0, len(team))
	for p := range team {
		puuids = append(puuids, p)
	}

	// Phase A: recent match IDs per player, fanned out.
	hist := make([][]string, len(puuids))
	runBounded(ctx, len(puuids), statsConcurrency, func(i int) {
		hist[i], _ = c.MatchHistory(ctx, puuids[i], queue, n)
	})

	// Keep only match IDs that appear in ≥2 players' histories (dedupe per player
	// first so one player listing a match twice can't fake a shared match).
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

	// Phase B: fetch only the shared matches, fanned out.
	var mu sync.Mutex
	past := make([]*MatchDetail, 0, len(shared))
	runBounded(ctx, len(shared), statsConcurrency, func(i int) {
		md, err := c.MatchDetails(ctx, shared[i])
		if err != nil {
			return
		}
		mu.Lock()
		past = append(past, md)
		mu.Unlock()
	})

	return InferParties(team, past)
}
