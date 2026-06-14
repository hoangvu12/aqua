package riot

// Live round score from the local presence feed. The Riot Client exposes every
// online presence (self + friends) at the chat presences resource, and for
// VALORANT each presence carries a base64-encoded JSON `private` blob. That blob
// is the *only* place Riot surfaces live in-match data outside the game itself —
// notably the round score — so we read our own presence to put a score header on
// the live scoreboard. Per-player live KDA is not in here (or any API); the blob
// has the team score and loop state, nothing more.

import (
	"context"
	"encoding/base64"
	"encoding/json"
)

// presencePaths are the local presence resources we try in order. The resource
// version drifts across client builds (the websocket event URI moved from
// /chat/v4 to /social/v1 over time — see events.go), so we probe a couple of
// known HTTP paths and use the first that answers. Same payload shape across
// them; parse defensively and degrade to "no score" on anything unexpected.
var presencePaths = []string{"/chat/v4/presences", "/chat/v6/presences"}

// MatchScore is the live round score read from the local player's own presence.
// Ally is our team's score: a party is always one team, so the party-owner
// perspective baked into the blob maps directly to our side. Valid is false when
// we're not in a running match or the presence/score couldn't be read.
type MatchScore struct {
	Ally  int
	Enemy int
	Valid bool
}

// presencePrivate is the subset of the base64-decoded presence `private` blob we
// use. Undocumented and patch-fragile → absent fields just stay zero.
type presencePrivate struct {
	SessionLoopState              string `json:"sessionLoopState"`              // MENUS|PREGAME|INGAME
	PartyOwnerMatchScoreAllyTeam  int    `json:"partyOwnerMatchScoreAllyTeam"`  // our team's rounds
	PartyOwnerMatchScoreEnemyTeam int    `json:"partyOwnerMatchScoreEnemyTeam"` // their rounds
}

// LiveMatchScore reads the local presences, finds our own VALORANT presence, and
// decodes the in-match round score from its private blob. Best-effort: any
// failure (versioning, not in a match, unexpected blob) returns Valid=false so
// the caller simply omits the score header. Cheap (localhost, not rate-limited).
func (c *Client) LiveMatchScore(ctx context.Context) MatchScore {
	for _, path := range presencePaths {
		var r struct {
			Presences []struct {
				PUUID   string `json:"puuid"`
				Private string `json:"private"`
			} `json:"presences"`
		}
		if err := c.localGet(ctx, path, &r); err != nil {
			continue // wrong version for this build → try the next path
		}
		for _, p := range r.Presences {
			if p.PUUID != c.auth.PUUID || p.Private == "" {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(p.Private)
			if err != nil {
				return MatchScore{}
			}
			var pv presencePrivate
			if err := json.Unmarshal(raw, &pv); err != nil {
				return MatchScore{}
			}
			if pv.SessionLoopState != "INGAME" {
				return MatchScore{} // pregame/menus → no live score to show
			}
			return MatchScore{
				Ally:  pv.PartyOwnerMatchScoreAllyTeam,
				Enemy: pv.PartyOwnerMatchScoreEnemyTeam,
				Valid: true,
			}
		}
		return MatchScore{} // path answered but our presence wasn't there → done
	}
	return MatchScore{}
}
