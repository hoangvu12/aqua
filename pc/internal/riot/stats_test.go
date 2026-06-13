package riot

import (
	"math"
	"reflect"
	"testing"
)

// TestAggregateStats locks in the tracker math: Win%/K-D/ADR/HS% over recent
// matches, that Recent preserves newest-first order, and that a match the
// player wasn't in (shared-match dedupe) is skipped.
func TestAggregateStats(t *testing.T) {
	const me = "p1"
	matches := []*MatchDetail{
		{Players: map[string]MatchPlayer{
			me: {Won: true, Kills: 20, Deaths: 10, Rounds: 20, Damage: 3000, Headshots: 30, Bodyshots: 60, Legshots: 10},
		}},
		{Players: map[string]MatchPlayer{
			me: {Won: false, Kills: 10, Deaths: 10, Rounds: 20, Damage: 2000, Headshots: 10, Bodyshots: 80, Legshots: 10},
		}},
		{Players: map[string]MatchPlayer{ // me not in this one → must be skipped
			"other": {Won: true, Kills: 99},
		}},
	}

	st := AggregateStats(me, matches)

	if st.Matches != 2 {
		t.Fatalf("Matches = %d, want 2", st.Matches)
	}
	if st.Wins != 1 || st.WinPct != 50 {
		t.Fatalf("Wins/WinPct = %d/%.1f, want 1/50", st.Wins, st.WinPct)
	}
	if !approx(st.KD, 1.5) { // 30 kills / 20 deaths
		t.Fatalf("KD = %v, want 1.5", st.KD)
	}
	if !approx(st.ADR, 125) { // 5000 damage / 40 rounds
		t.Fatalf("ADR = %v, want 125", st.ADR)
	}
	if !approx(st.HSPct, 20) { // 40 hs / (40+140+20) shots
		t.Fatalf("HSPct = %v, want 20", st.HSPct)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(st.Recent, want) {
		t.Fatalf("Recent = %v, want %v (newest first)", st.Recent, want)
	}
}

// TestAggregateStatsZeroDeaths guards the div-by-zero path: no deaths means K/D
// degrades to the kill count rather than +Inf/NaN.
func TestAggregateStatsZeroDeaths(t *testing.T) {
	const me = "p1"
	st := AggregateStats(me, []*MatchDetail{
		{Players: map[string]MatchPlayer{me: {Won: true, Kills: 7, Deaths: 0, Rounds: 13}}},
	})
	if !approx(st.KD, 7) {
		t.Fatalf("KD = %v, want 7", st.KD)
	}
}

func approx(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
