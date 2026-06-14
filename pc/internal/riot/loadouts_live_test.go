package riot

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
)

// TestLiveLoadouts dumps the loadout (skins/sprays) for everyone in your current
// match and resolves the cosmetic UUIDs to names via valorant-api, so we can see
// the real response shape before building the scoreboard feature on top of it.
// Prefers core-game (both teams); falls back to pregame (allies only).
//
//	AQUA_LIVE_LOADOUTS=1 go -C pc test ./internal/riot -run TestLiveLoadouts -v -count=1
func TestLiveLoadouts(t *testing.T) {
	if os.Getenv("AQUA_LIVE_LOADOUTS") != "1" {
		t.Skip("set AQUA_LIVE_LOADOUTS=1 with VALORANT in a match/agent-select")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := Authenticate(ctx)
	if err != nil {
		t.Skipf("authenticate (Riot Client running?): %v", err)
	}

	// Find the live match (both teams) or fall back to agent select (allies only).
	var path string
	if mid, err := c.CoreGamePlayer(ctx); err == nil && mid != "" {
		path = "/core-game/v1/matches/" + mid + "/loadouts"
		t.Logf("core-game match %s", mid)
	} else if mid, err := c.PregamePlayer(ctx); err == nil && mid != "" {
		path = "/pregame/v1/matches/" + mid + "/loadouts"
		t.Logf("pregame match %s (allies only)", mid)
	} else {
		t.Skip("not in a match or agent select")
	}

	// The documented shape (current-game-loadouts). Pregame nests the same Loadout
	// under a "Loadouts[].Loadout" too, so this struct fits both.
	var resp struct {
		Loadouts []struct {
			CharacterID string `json:"CharacterID"`
			Loadout     struct {
				Subject string `json:"Subject"`
				Sprays  struct {
					SpraySelections []struct {
						SocketID string `json:"SocketID"`
						SprayID  string `json:"SprayID"`
					} `json:"SpraySelections"`
				} `json:"Sprays"`
				Items map[string]struct {
					ID      string `json:"ID"`
					TypeID  string `json:"TypeID"`
					Sockets map[string]struct {
						ID   string `json:"ID"`
						Item struct {
							ID     string `json:"ID"`
							TypeID string `json:"TypeID"`
						} `json:"Item"`
					} `json:"Sockets"`
				} `json:"Items"`
			} `json:"Loadout"`
		} `json:"Loadouts"`
	}
	if err := c.glz(ctx, "GET", c.glzURL(path), &resp); err != nil {
		t.Fatalf("loadouts: %v", err)
	}
	t.Logf("got %d loadouts", len(resp.Loadouts))

	// Resolve cosmetic UUIDs → names from valorant-api (public, no auth).
	skinName := va[skinsEntry](t, c, "weapons/skins")    // skin / chroma / level uuids
	weaponName := va[namedEntry](t, c, "weapons")        // weapon uuid → "Vandal"
	agentName := va[namedEntry](t, c, "agents")          // character uuid → "Jett"
	sprayName := va[namedEntry](t, c, "sprays")          // spray uuid → "GG Spray"

	names, _ := c.Names(ctx, subjectsOf(resp.Loadouts))

	for _, l := range resp.Loadouts {
		who := names[l.Loadout.Subject]
		if who == "" {
			who = l.Loadout.Subject[:8]
		}
		t.Logf("── %s  (%s)", who, agentName[l.CharacterID])

		// Weapons, sorted by name for stable output. The skin lives in whichever
		// socket whose Item.ID resolves against the skins map.
		type wk struct{ weapon, skin string }
		var rows []wk
		for weaponID, item := range l.Loadout.Items {
			skin := ""
			for _, sock := range item.Sockets {
				if n := skinName[sock.Item.ID]; n != "" {
					skin = n // prefer the most specific (level/chroma) name available
				}
			}
			rows = append(rows, wk{weapon: weaponName[weaponID], skin: skin})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].weapon < rows[j].weapon })
		for _, r := range rows {
			if r.skin != "" {
				t.Logf("     %-12s %s", r.weapon, r.skin)
			}
		}
		var sprays []string
		for _, s := range l.Loadout.Sprays.SpraySelections {
			if n := sprayName[s.SprayID]; n != "" {
				sprays = append(sprays, n)
			}
		}
		if len(sprays) > 0 {
			t.Logf("     sprays:      %v", sprays)
		}
	}

	// Dump the first player's raw loadout so the exact socket structure is visible.
	if len(resp.Loadouts) > 0 {
		raw, _ := json.MarshalIndent(resp.Loadouts[0], "", "  ")
		t.Logf("\nraw[0]:\n%s", raw)
	}
}

type namedEntry struct {
	UUID        string `json:"uuid"`
	DisplayName string `json:"displayName"`
}

type skinsEntry struct {
	UUID        string       `json:"uuid"`
	DisplayName string       `json:"displayName"`
	Chromas     []namedEntry `json:"chromas"`
	Levels      []namedEntry `json:"levels"`
}

// va fetches a valorant-api list endpoint and flattens it into a uuid→name map.
// For skins it also folds in every chroma + level uuid (those are what loadout
// sockets reference), so any of them resolves to a readable name.
func va[T any](t *testing.T, c *Client, endpoint string) map[string]string {
	req, _ := http.NewRequest("GET", "https://valorant-api.com/v1/"+endpoint, nil)
	var r struct {
		Data []T `json:"data"`
	}
	if err := do(c.remote, req, &r); err != nil {
		t.Logf("valorant-api %s: %v (names will be blank)", endpoint, err)
		return map[string]string{}
	}
	m := map[string]string{}
	for _, e := range r.Data {
		switch v := any(e).(type) {
		case namedEntry:
			m[v.UUID] = v.DisplayName
		case skinsEntry:
			m[v.UUID] = v.DisplayName
			for _, ch := range v.Chromas {
				m[ch.UUID] = v.DisplayName // chroma names are noisy; use the skin name
			}
			for _, lv := range v.Levels {
				m[lv.UUID] = v.DisplayName
			}
		}
	}
	return m
}

func subjectsOf[T any](loadouts []T) []string {
	// Subjects are read reflectively-free via a tiny re-marshal to keep the probe
	// simple; loadouts is small (≤10) so the cost is irrelevant.
	var out []string
	b, _ := json.Marshal(loadouts)
	var generic []struct {
		Loadout struct {
			Subject string `json:"Subject"`
		} `json:"Loadout"`
	}
	_ = json.Unmarshal(b, &generic)
	for _, g := range generic {
		if g.Loadout.Subject != "" {
			out = append(out, g.Loadout.Subject)
		}
	}
	return out
}
