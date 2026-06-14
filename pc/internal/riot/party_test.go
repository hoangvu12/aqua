package riot

import (
	"reflect"
	"testing"
)

// md builds a MatchDetail from puuid→partyId pairs (team is irrelevant to the
// historical match; only the shared partyId matters for inference).
func md(id string, party map[string]string) *MatchDetail {
	m := &MatchDetail{MatchID: id, Players: map[string]MatchPlayer{}}
	for puuid, pid := range party {
		m.Players[puuid] = MatchPlayer{PartyID: pid}
	}
	return m
}

func TestInferParties(t *testing.T) {
	// Current match: A,B,C on Blue; D,E on Red.
	team := map[string]string{"A": "Blue", "B": "Blue", "C": "Blue", "D": "Red", "E": "Red"}

	t.Run("links players who shared a partyId", func(t *testing.T) {
		past := []*MatchDetail{
			md("m1", map[string]string{"A": "px", "B": "px", "Z": "px"}), // A,B premade (Z not in current match)
		}
		got := InferParties(team, past)
		want := [][]string{{"A", "B"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("transitive trio across two matches", func(t *testing.T) {
		past := []*MatchDetail{
			md("m1", map[string]string{"A": "p1", "B": "p1"}),
			md("m2", map[string]string{"B": "p2", "C": "p2"}),
		}
		got := InferParties(team, past)
		want := [][]string{{"A", "B", "C"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("same past match but different parties → not linked", func(t *testing.T) {
		past := []*MatchDetail{
			md("m1", map[string]string{"A": "p1", "D": "p2"}), // together in a match, different parties
		}
		if got := InferParties(team, past); len(got) != 0 {
			t.Errorf("got %v, want no groups", got)
		}
	})

	t.Run("partied before but enemies now → not linked", func(t *testing.T) {
		// A (Blue) and D (Red) shared a party in the past but are opponents now.
		past := []*MatchDetail{md("m1", map[string]string{"A": "p1", "D": "p1"})}
		if got := InferParties(team, past); len(got) != 0 {
			t.Errorf("got %v, want no groups (cross-team)", got)
		}
	})

	t.Run("solos produce nothing", func(t *testing.T) {
		past := []*MatchDetail{md("m1", map[string]string{"A": "p1", "D": "p2", "C": "p3"})}
		if got := InferParties(team, past); len(got) != 0 {
			t.Errorf("got %v, want no groups", got)
		}
	})

	t.Run("two independent parties", func(t *testing.T) {
		past := []*MatchDetail{
			md("m1", map[string]string{"A": "p1", "B": "p1"}), // Blue duo
			md("m2", map[string]string{"D": "p9", "E": "p9"}), // Red duo
		}
		got := InferParties(team, past)
		want := [][]string{{"A", "B"}, {"D", "E"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
