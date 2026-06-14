package riot

import (
	"context"
	"net/http"
)

// The in-match loadout exposes every player's equipped cosmetics. We surface a
// curated set of guns (the ones people actually notice + flex) so the phone's
// scoreboard stays glanceable — a skin collector runs a skin on all 17 weapons,
// which would bury the row. Order here is the display order on the phone.
var loadoutWeapons = []struct{ ID, Label string }{
	{"9c82e19d-4575-0200-1a81-3eacf00cf872", "Vandal"},
	{"ee8e8d15-496b-07ac-e5f6-8fae5d4c7b1a", "Phantom"},
	{"a03b24d3-4319-996d-0f8c-94bbfba1dfc7", "Operator"},
	{"e336c6b8-418d-9340-d77f-7a9e4cfe0702", "Sheriff"},
	{"29a0cfab-485b-f5d5-779a-b59f85e204a8", "Classic"},
	{"2f59173c-4bed-b6c3-2191-dea9b58be9c7", "Knife"},
}

// Each weapon item carries sockets; this one holds the equipped skin. Its
// Item.ID is the top-level skin UUID (verified against valorant-api), so a single
// lookup resolves it — no need to walk the level/chroma sockets.
const skinSocketID = "bcef87d6-209b-46c6-8b19-fbe40bd95abc"

// Default skins (the stock gun + "Random Favorite") aren't worth showing. We drop
// them by theme so a player with no real skin on a gun just omits that row.
var defaultSkinThemes = map[string]bool{
	"5a629df4-4765-0214-bd40-fbb96542941f": true, // Standard
	"0d7a5bfb-4850-098e-1821-d989bbfd58a8": true, // Random Favorite
}

// EquippedSkin is one gun's equipped skin (still an opaque UUID; resolve via
// WeaponSkins to get the name + render).
type EquippedSkin struct {
	Weapon string // display label ("Vandal", "Knife", …)
	SkinID string // top-level skin UUID
}

// MatchLoadouts returns the equipped skin of each curated weapon, per player, for
// a live match. Undocumented + patch-fragile → parse defensively; callers degrade
// to "no skins" on any failure. Keyed by player subject (puuid).
func (c *Client) MatchLoadouts(ctx context.Context, matchID string) (map[string][]EquippedSkin, error) {
	var resp struct {
		Loadouts []struct {
			Loadout struct {
				Subject string `json:"Subject"`
				Items   map[string]struct {
					Sockets map[string]struct {
						Item struct {
							ID string `json:"ID"`
						} `json:"Item"`
					} `json:"Sockets"`
				} `json:"Items"`
			} `json:"Loadout"`
		} `json:"Loadouts"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/core-game/v1/matches/"+matchID+"/loadouts"), &resp); err != nil {
		return nil, err
	}
	out := make(map[string][]EquippedSkin, len(resp.Loadouts))
	for _, l := range resp.Loadouts {
		var skins []EquippedSkin
		for _, w := range loadoutWeapons {
			item, ok := l.Loadout.Items[w.ID]
			if !ok {
				continue
			}
			if sock, ok := item.Sockets[skinSocketID]; ok && sock.Item.ID != "" {
				skins = append(skins, EquippedSkin{Weapon: w.Label, SkinID: sock.Item.ID})
			}
		}
		if l.Loadout.Subject != "" {
			out[l.Loadout.Subject] = skins
		}
	}
	return out, nil
}

// SkinInfo is a resolved skin: a display name and a transparent gun render.
type SkinInfo struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// WeaponSkins fetches the valorant-api skin catalog and indexes it by skin UUID,
// dropping default-theme skins (so an unresolved lookup == "stock gun, skip it").
// Public valorant-api data, no auth. Names are left in English: skin names are
// brand names that don't meaningfully localize, and this keeps the fetch robust.
func (c *Client) WeaponSkins(ctx context.Context) (map[string]SkinInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://valorant-api.com/v1/weapons/skins", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []struct {
			UUID        string `json:"uuid"`
			DisplayName string `json:"displayName"`
			DisplayIcon string `json:"displayIcon"`
			ThemeUUID   string `json:"themeUuid"`
			Levels      []struct {
				DisplayIcon string `json:"displayIcon"`
			} `json:"levels"`
			Chromas []struct {
				FullRender string `json:"fullRender"`
			} `json:"chromas"`
		} `json:"data"`
	}
	if err := do(c.remote, req, &r); err != nil {
		return nil, err
	}
	m := make(map[string]SkinInfo, len(r.Data))
	for _, s := range r.Data {
		if defaultSkinThemes[s.ThemeUUID] {
			continue
		}
		img := s.DisplayIcon
		if img == "" { // some skins only carry art on their levels/chromas
			for i := len(s.Levels) - 1; i >= 0 && img == ""; i-- {
				img = s.Levels[i].DisplayIcon
			}
		}
		if img == "" && len(s.Chromas) > 0 {
			img = s.Chromas[0].FullRender
		}
		if img == "" {
			continue // nothing to render → not worth a row
		}
		m[s.UUID] = SkinInfo{Name: s.DisplayName, Image: img}
	}
	return m, nil
}
