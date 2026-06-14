package riot

// Tracker-style player stats: identity (name), standing (current + peak rank),
// and recent-form aggregates (Win%/K-D/ADR/HS%) computed from match history.
// All of this is read-only PD-host data and works for any PUUID the game hands
// us (party members, agent-select allies, both teams in core-game), so no
// third-party tracker API is needed — see the riot package doc for the auth path.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
)

// statsConcurrency caps simultaneous PD requests during a lobby fetch — these
// endpoints are rate-limited, so we fan out politely rather than all-at-once.
const statsConcurrency = 6

// PlayerStats is the per-player line the phone renders (one row in the lobby /
// agent-select / scoreboard list).
type PlayerStats struct {
	PUUID    string  `json:"puuid"`
	Name     string  `json:"name"`      // "GameName#TagLine" ("" if hidden/unresolved)
	Tier     int     `json:"tier"`      // current competitive tier (0 = unranked)
	RR       int     `json:"rr"`        // ranked rating within the current tier
	PeakTier int     `json:"peak_tier"` // highest tier ever won a game at
	Matches  int     `json:"matches"`   // matches counted in the aggregates below
	Wins     int     `json:"wins"`
	WinPct   float64 `json:"win_pct"` // 0..100
	KD       float64 `json:"kd"`
	ADR      float64 `json:"adr"`    // average damage per round
	HSPct    float64 `json:"hs_pct"` // 0..100
	Recent   []bool  `json:"recent"` // recent results, newest first (true = win)
}

// ---- name-service (PD) ---------------------------------------------------

// Names resolves PUUIDs to "GameName#TagLine" in one batched PUT. Players who
// are hidden or unresolved simply won't appear in the returned map.
func (c *Client) Names(ctx context.Context, puuids []string) (map[string]string, error) {
	if len(puuids) == 0 {
		return map[string]string{}, nil
	}
	var entries []struct {
		Subject  string `json:"Subject"`
		GameName string `json:"GameName"`
		TagLine  string `json:"TagLine"`
	}
	if err := c.glzBody(ctx, "PUT", c.pdURL("/name-service/v2/players"), puuids, &entries); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		name := e.GameName
		if e.TagLine != "" {
			name += "#" + e.TagLine
		}
		out[e.Subject] = name
	}
	return out, nil
}

// ---- mmr / rank (PD) -----------------------------------------------------

// RankInfo is a player's current and peak competitive standing.
type RankInfo struct {
	Tier     int // current competitive tier (0 = unranked)
	RR       int // ranked rating within the current tier
	PeakTier int // highest tier with at least one win, across all acts
}

// PlayerMMR fetches current + peak rank for any PUUID. Peak is derived from each
// act's WinsByTier (the highest tier the player actually won a game at) rather
// than the act's ending CompetitiveTier, which undershoots — this matches the
// in-client peak/act-rank badge. A player who never played competitive can 404
// → ErrNotFound; callers treat that as unranked.
func (c *Client) PlayerMMR(ctx context.Context, puuid string) (RankInfo, error) {
	var r struct {
		LatestCompetitiveUpdate struct {
			TierAfterUpdate         int `json:"TierAfterUpdate"`
			RankedRatingAfterUpdate int `json:"RankedRatingAfterUpdate"`
		} `json:"LatestCompetitiveUpdate"`
		QueueSkills struct {
			Competitive struct {
				SeasonalInfoBySeasonID map[string]struct {
					CompetitiveTier int            `json:"CompetitiveTier"`
					WinsByTier       map[string]int `json:"WinsByTier"`
				} `json:"SeasonalInfoBySeasonID"`
			} `json:"competitive"`
		} `json:"QueueSkills"`
	}
	if err := c.glz(ctx, "GET", c.pdURL("/mmr/v1/players/"+puuid), &r); err != nil {
		return RankInfo{}, err
	}
	info := RankInfo{
		Tier: r.LatestCompetitiveUpdate.TierAfterUpdate,
		RR:   r.LatestCompetitiveUpdate.RankedRatingAfterUpdate,
	}
	info.PeakTier = info.Tier
	for _, s := range r.QueueSkills.Competitive.SeasonalInfoBySeasonID {
		for tierStr, wins := range s.WinsByTier {
			if wins <= 0 {
				continue
			}
			if t, err := strconv.Atoi(tierStr); err == nil && t > info.PeakTier {
				info.PeakTier = t
			}
		}
	}
	return info, nil
}

// ---- match history & details (PD) ---------------------------------------

// MatchHistory returns recent match IDs for a player, newest first. queue is an
// optional filter ("competitive", "unrated", …); empty means all queues.
func (c *Client) MatchHistory(ctx context.Context, puuid, queue string, count int) ([]string, error) {
	if count <= 0 {
		count = 10
	}
	path := fmt.Sprintf("/match-history/v1/history/%s?startIndex=0&endIndex=%d", puuid, count)
	if queue != "" {
		path += "&queue=" + queue
	}
	var r struct {
		History []struct {
			MatchID string `json:"MatchID"`
		} `json:"History"`
	}
	if err := c.glz(ctx, "GET", c.pdURL(path), &r); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(r.History))
	for _, h := range r.History {
		ids = append(ids, h.MatchID)
	}
	return ids, nil
}

// MatchPlayer is one player's line in a single match, reduced to the numbers we
// aggregate (damage/shots are summed across every round).
type MatchPlayer struct {
	Team      string
	PartyID   string // the party this player was in *for this match* (premade detection)
	Won       bool
	Kills     int
	Deaths    int
	Assists   int
	Rounds    int
	Damage    int
	Headshots int
	Bodyshots int
	Legshots  int
}

// MatchDetail is the slice of a match we keep: per-player lines keyed by PUUID.
// One fetch covers every player in the match, so a lobby aggregator should
// dedupe match IDs and fetch each match only once.
type MatchDetail struct {
	MatchID string
	Players map[string]MatchPlayer
}

// matchDetailsCacheMax bounds the in-memory match-details cache. Match details
// never change once a match is over, so we keep them for the session; this cap is
// just a backstop against unbounded growth over a very long run (each entry is a
// small ~10-player map). On overflow the cache is dropped wholesale — crude but
// match details are cheap to refetch and this path is rarely hit.
const matchDetailsCacheMax = 1000

// MatchDetails returns a match's per-player lines, caching by match id so a match
// shared across LobbyStats and DetectParties (or refetched later) hits PD only
// once. The returned value is shared and treated as read-only by callers.
func (c *Client) MatchDetails(ctx context.Context, matchID string) (*MatchDetail, error) {
	c.mdMu.Lock()
	cached, ok := c.mdCache[matchID]
	c.mdMu.Unlock()
	if ok {
		return cached, nil
	}

	md, err := c.fetchMatchDetails(ctx, matchID)
	if err != nil {
		return nil, err
	}

	c.mdMu.Lock()
	if len(c.mdCache) >= matchDetailsCacheMax {
		c.mdCache = make(map[string]*MatchDetail, matchDetailsCacheMax)
	}
	c.mdCache[matchID] = md
	c.mdMu.Unlock()
	return md, nil
}

// fetchMatchDetails does the actual PD fetch and reduces a full match to per-player
// MatchPlayer lines (KDA + rounds from stats, damage/headshots summed per round).
func (c *Client) fetchMatchDetails(ctx context.Context, matchID string) (*MatchDetail, error) {
	var r struct {
		Players []struct {
			Subject string `json:"subject"`
			TeamID  string `json:"teamId"`
			PartyID string `json:"partyId"`
			Stats   struct {
				RoundsPlayed int `json:"roundsPlayed"`
				Kills        int `json:"kills"`
				Deaths       int `json:"deaths"`
				Assists      int `json:"assists"`
			} `json:"stats"`
		} `json:"players"`
		Teams []struct {
			TeamID string `json:"teamId"`
			Won    bool   `json:"won"`
		} `json:"teams"`
		RoundResults []struct {
			PlayerStats []struct {
				Subject string `json:"subject"`
				Damage  []struct {
					Damage    int `json:"damage"`
					Legshots  int `json:"legshots"`
					Bodyshots int `json:"bodyshots"`
					Headshots int `json:"headshots"`
				} `json:"damage"`
			} `json:"playerStats"`
		} `json:"roundResults"`
	}
	if err := c.glz(ctx, "GET", c.pdURL("/match-details/v1/matches/"+matchID), &r); err != nil {
		return nil, err
	}

	won := make(map[string]bool, len(r.Teams))
	for _, t := range r.Teams {
		won[t.TeamID] = t.Won
	}
	md := &MatchDetail{MatchID: matchID, Players: make(map[string]MatchPlayer, len(r.Players))}
	for _, p := range r.Players {
		md.Players[p.Subject] = MatchPlayer{
			Team:    p.TeamID,
			PartyID: p.PartyID,
			Won:     won[p.TeamID],
			Kills:   p.Stats.Kills,
			Deaths:  p.Stats.Deaths,
			Assists: p.Stats.Assists,
			Rounds:  p.Stats.RoundsPlayed,
		}
	}
	for _, rr := range r.RoundResults {
		for _, ps := range rr.PlayerStats {
			mp, ok := md.Players[ps.Subject]
			if !ok {
				continue
			}
			for _, d := range ps.Damage {
				mp.Damage += d.Damage
				mp.Headshots += d.Headshots
				mp.Bodyshots += d.Bodyshots
				mp.Legshots += d.Legshots
			}
			md.Players[ps.Subject] = mp
		}
	}
	return md, nil
}

// ---- aggregation ---------------------------------------------------------

// AggregateStats reduces a player's recent matches into a tracker line. matches
// should be newest-first; Recent mirrors that order. Pure (no I/O), so the
// orchestration layer can fetch + cache match details however it likes and reuse
// one MatchDetail across every player who shared that match.
func AggregateStats(puuid string, matches []*MatchDetail) PlayerStats {
	st := PlayerStats{PUUID: puuid}
	var kills, deaths, damage, rounds, hs, body, leg int
	for _, m := range matches {
		mp, ok := m.Players[puuid]
		if !ok {
			continue // player wasn't in this match (shared-match dedupe)
		}
		st.Matches++
		if mp.Won {
			st.Wins++
		}
		st.Recent = append(st.Recent, mp.Won)
		kills += mp.Kills
		deaths += mp.Deaths
		damage += mp.Damage
		rounds += mp.Rounds
		hs += mp.Headshots
		body += mp.Bodyshots
		leg += mp.Legshots
	}
	if st.Matches > 0 {
		st.WinPct = 100 * float64(st.Wins) / float64(st.Matches)
	}
	if deaths > 0 {
		st.KD = float64(kills) / float64(deaths)
	} else {
		st.KD = float64(kills) // no deaths → K/D is just kills (avoid div-by-zero)
	}
	if rounds > 0 {
		st.ADR = float64(damage) / float64(rounds)
	}
	if shots := hs + body + leg; shots > 0 {
		st.HSPct = 100 * float64(hs) / float64(shots)
	}
	return st
}

// LobbyStats fetches tracker rows for a whole lobby/match at once, keyed by
// PUUID. It is the efficient path for the scoreboard: names resolve in one
// batched call, history + rank fan out per player, and match details are
// deduped so a match two players shared is fetched only once. n is how many
// recent matches to weigh per player. Best-effort throughout — a player whose
// data fails simply gets a sparse row rather than failing the whole fetch.
//
// Still heavy and PD-rate-limited: call it once per match and cache the result;
// don't drive it from the poll loop.
func (c *Client) LobbyStats(ctx context.Context, puuids []string, queue string, n int) map[string]PlayerStats {
	out := make(map[string]PlayerStats, len(puuids))
	if len(puuids) == 0 {
		return out
	}
	if n <= 0 {
		n = 8
	}

	// One batched name lookup for everyone.
	names, _ := c.Names(ctx, puuids)

	// Phase A: per-player match history + rank, fanned out.
	type playerData struct {
		ids  []string
		rank RankInfo
	}
	data := make([]playerData, len(puuids))
	runBounded(ctx, len(puuids), statsConcurrency, func(i int) {
		ids, _ := c.MatchHistory(ctx, puuids[i], queue, n)
		rank, _ := c.PlayerMMR(ctx, puuids[i]) // ErrNotFound → zero value (unranked)
		data[i] = playerData{ids: ids, rank: rank}
	})

	// Dedupe match IDs across everyone so a shared match is fetched once.
	idSet := map[string]struct{}{}
	for _, d := range data {
		for _, id := range d.ids {
			idSet[id] = struct{}{}
		}
	}
	uniq := make([]string, 0, len(idSet))
	for id := range idSet {
		uniq = append(uniq, id)
	}

	// Phase B: fetch each unique match once, fanned out.
	var detMu sync.Mutex
	details := make(map[string]*MatchDetail, len(uniq))
	runBounded(ctx, len(uniq), statsConcurrency, func(i int) {
		md, err := c.MatchDetails(ctx, uniq[i])
		if err != nil {
			return // skip unreadable matches
		}
		detMu.Lock()
		details[uniq[i]] = md
		detMu.Unlock()
	})

	// Phase C: aggregate per player (cheap, no I/O) over the shared details.
	for i, p := range puuids {
		ms := make([]*MatchDetail, 0, len(data[i].ids))
		for _, id := range data[i].ids {
			if md := details[id]; md != nil {
				ms = append(ms, md)
			}
		}
		st := AggregateStats(p, ms)
		st.Name = names[p]
		st.Tier, st.RR, st.PeakTier = data[i].rank.Tier, data[i].rank.RR, data[i].rank.PeakTier
		out[p] = st
	}
	return out
}

// runBounded runs fn(0..n-1) concurrently, at most max in flight. It returns
// once all have finished (or ctx is cancelled). Index-keyed so callers write
// results into a pre-sized slice without a shared mutex.
func runBounded(ctx context.Context, n, max int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if max < 1 {
		max = 1
	}
	sem := make(chan struct{}, max)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
