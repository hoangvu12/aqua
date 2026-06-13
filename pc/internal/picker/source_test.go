package picker

import (
	"reflect"
	"testing"

	"aqua/internal/riot"
)

// TestMissingPUUIDs covers the roster-growth case that left enemies stuck on
// "Loading": allies cached in agent select, both teams requested in core-game.
func TestMissingPUUIDs(t *testing.T) {
	allies := map[string]riot.PlayerStats{
		"ally-1": {}, "ally-2": {},
	}
	cases := []struct {
		name string
		have map[string]riot.PlayerStats
		want []string
		miss []string
	}{
		{"empty cache → all missing", nil, []string{"a", "b"}, []string{"a", "b"}},
		{"roster grows → only newcomers", allies, []string{"ally-1", "ally-2", "enemy-1", "enemy-2"}, []string{"enemy-1", "enemy-2"}},
		{"all cached → none", allies, []string{"ally-1", "ally-2"}, nil},
		{"blank puuids skipped", nil, []string{"", "a", ""}, []string{"a"}},
		// A sparse row (fetch returned little) still counts as present, so the
		// player isn't re-requested in a refetch loop.
		{"sparse row not re-requested", map[string]riot.PlayerStats{"a": {}}, []string{"a"}, nil},
	}
	for _, c := range cases {
		if got := missingPUUIDs(c.have, c.want); !reflect.DeepEqual(got, c.miss) {
			t.Errorf("%s: missingPUUIDs = %v, want %v", c.name, got, c.miss)
		}
	}
}
