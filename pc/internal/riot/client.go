// Package riot is the Go port of the reference agent-picker protocol: read the
// Riot Client lockfile, fetch local entitlements + region, and call the
// undocumented GLZ/PD pvp.net endpoints with the exact headers Riot expects.
// These endpoints are patch-fragile, so callers parse defensively.
//
// ⚠️ select/lock automate agent select (instalocker class) — ban risk per the
// plan's Decision 9. Read-only polling is low risk; the lock is not.
package riot

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Agent items in the store entitlements live under this ItemTypeID.
const agentItemTypeID = "01bb38e1-da47-4e6a-9b3d-945fe4655707"

const fallbackClientVersion = "release-09.00-shipping-0-0000000"

// X-Riot-ClientPlatform is a fixed base64 blob describing a Windows PC client.
var platformHeaderB64 = base64.StdEncoding.EncodeToString([]byte(
	`{"platformType":"PC","platformOS":"Windows","platformOSVersion":"10.0.19042.1.256.64bit","platformChipset":"Unknown"}`))

// Sentinels let callers branch on HTTP outcomes without inspecting status codes.
var (
	ErrNotFound     = errors.New("riot: not found") // 404 (e.g. not in pregame)
	ErrUnauthorized = errors.New("riot: unauthorized")
)

// Auth is the resolved session: tokens, identity, and the derived endpoints.
type Auth struct {
	AccessToken      string
	EntitlementToken string
	PUUID            string
	Region           string
	Locale           string
	Endpoints        Endpoints
}

// Client is an authenticated handle to the local + GLZ/PD APIs.
type Client struct {
	auth          Auth
	clientVersion string
	basic         string // local "Basic ..." header
	port          string
	local         *http.Client // 127.0.0.1, self-signed → InsecureSkipVerify
	remote        *http.Client // pvp.net, valid certs
}

// Authenticate reads the lockfile, fetches local entitlements + region-locale,
// resolves endpoints, and looks up the current client version.
func Authenticate(ctx context.Context) (*Client, error) {
	lf, err := ReadLockfile()
	if err != nil {
		return nil, err
	}
	c := &Client{
		port:  lf.Port,
		basic: "Basic " + base64.StdEncoding.EncodeToString([]byte("riot:"+lf.Password)),
		local: &http.Client{
			Timeout:   8 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
		remote: &http.Client{Timeout: 10 * time.Second},
	}

	var ent struct {
		AccessToken string `json:"accessToken"`
		Token       string `json:"token"`
		Subject     string `json:"subject"`
	}
	if err := c.localGet(ctx, "/entitlements/v1/token", &ent); err != nil {
		return nil, fmt.Errorf("entitlements: %w", err)
	}
	var rl struct {
		Region string `json:"region"`
		Locale string `json:"locale"`
	}
	if err := c.localGet(ctx, "/riotclient/region-locale", &rl); err != nil {
		return nil, fmt.Errorf("region-locale: %w", err)
	}

	c.auth = Auth{
		AccessToken:      ent.AccessToken,
		EntitlementToken: ent.Token,
		PUUID:            ent.Subject,
		Region:           strings.ToLower(rl.Region),
		Locale:           rl.Locale,
		Endpoints:        MapRegion(rl.Region),
	}
	c.clientVersion = c.fetchClientVersion(ctx)
	return c, nil
}

// PUUID is the authenticated player's id.
func (c *Client) PUUID() string { return c.auth.PUUID }

// Locale is the in-game locale (e.g. vi-VN), used to pick the phone language.
func (c *Client) Locale() string { return c.auth.Locale }

// ---- GLZ / PD calls ------------------------------------------------------

// PregamePlayer reports whether the player is in agent select. ErrNotFound (404)
// means not in pregame.
func (c *Client) PregamePlayer(ctx context.Context) (matchID string, err error) {
	var r struct {
		MatchID string `json:"MatchID"`
	}
	err = c.glz(ctx, "GET", c.glzURL("/pregame/v1/players/"+c.auth.PUUID), &r)
	return r.MatchID, err
}

// PregameMatch is the agent-select snapshot for a match.
type PregameMatch struct {
	ID                   string `json:"ID"`
	MapID                string `json:"MapID"`
	QueueID              string `json:"QueueID"`
	PhaseTimeRemainingNS int64  `json:"PhaseTimeRemainingNS"`
	AllyTeam             struct {
		Players []struct {
			Subject                 string `json:"Subject"`
			CharacterID             string `json:"CharacterID"`
			CharacterSelectionState string `json:"CharacterSelectionState"`
			PlayerIdentity          struct {
				Subject   string `json:"Subject"`
				Incognito bool   `json:"Incognito"`
			} `json:"PlayerIdentity"`
		} `json:"Players"`
	} `json:"AllyTeam"`
}

// PregameMatch fetches the agent-select state for a match id.
func (c *Client) PregameMatch(ctx context.Context, matchID string) (*PregameMatch, error) {
	var m PregameMatch
	if err := c.glz(ctx, "GET", c.glzURL("/pregame/v1/matches/"+matchID), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// PartyInfo is the pre-match party state used to tell menus/lobby/queue/
// matchfound apart (the GLZ pregame/core-game checks only cover pregame/ingame).
type PartyInfo struct {
	State      string // DEFAULT|MATCHMAKING|MATCHMADE|CUSTOM_GAME_SETUP|…
	QueueID    string // selected queue (empty in the bare main menu)
	ReadyCheck string // ""|None|InProgress (InProgress = match found, ready-check up)
}

// CurrentParty returns the player's current party id (everyone is always in a
// party, even solo). ErrNotFound is possible right at login.
func (c *Client) CurrentParty(ctx context.Context) (string, error) {
	var r struct {
		CurrentPartyID string `json:"CurrentPartyID"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/parties/v1/players/"+c.auth.PUUID), &r); err != nil {
		return "", err
	}
	return r.CurrentPartyID, nil
}

// Party fetches a party's matchmaking + ready-check state. Undocumented and
// patch-fragile (the ReadyCheck path especially) → callers parse defensively
// and degrade to plain "menus" on any failure.
func (c *Client) Party(ctx context.Context, id string) (PartyInfo, error) {
	var r struct {
		State           string `json:"State"`
		MatchmakingData struct {
			QueueID    string `json:"QueueID"`
			ReadyCheck struct {
				State string `json:"State"`
			} `json:"ReadyCheck"`
		} `json:"MatchmakingData"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/parties/v1/parties/"+id), &r); err != nil {
		return PartyInfo{}, err
	}
	return PartyInfo{
		State:      r.State,
		QueueID:    r.MatchmakingData.QueueID,
		ReadyCheck: r.MatchmakingData.ReadyCheck.State,
	}, nil
}

// InCoreGame reports whether the player is in an active match. ErrNotFound → no.
func (c *Client) InCoreGame(ctx context.Context) (bool, error) {
	err := c.glz(ctx, "GET", c.glzURL("/core-game/v1/players/"+c.auth.PUUID), nil)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CoreGamePlayer returns the player's live match id (ErrNotFound → not in a
// running match). Unlike InCoreGame it hands back the id needed for the roster.
func (c *Client) CoreGamePlayer(ctx context.Context) (string, error) {
	var r struct {
		MatchID string `json:"MatchID"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/core-game/v1/players/"+c.auth.PUUID), &r); err != nil {
		return "", err
	}
	return r.MatchID, nil
}

// CoreGameSeat is one player in a live match. Unlike pregame, core-game exposes
// BOTH teams, so this is the source for the enemy roster (the scoreboard).
type CoreGameSeat struct {
	Subject     string
	TeamID      string // "Blue" | "Red"
	CharacterID string
	Incognito   bool // streamer mode — honor by redacting name/level
}

// CoreGameMatch returns every player in a live match (both teams). Undocumented
// and patch-fragile → parse defensively; callers degrade gracefully.
func (c *Client) CoreGameMatch(ctx context.Context, matchID string) ([]CoreGameSeat, error) {
	var r struct {
		Players []struct {
			Subject        string `json:"Subject"`
			TeamID         string `json:"TeamID"`
			CharacterID    string `json:"CharacterID"`
			PlayerIdentity struct {
				Incognito bool `json:"Incognito"`
			} `json:"PlayerIdentity"`
		} `json:"Players"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/core-game/v1/matches/"+matchID), &r); err != nil {
		return nil, err
	}
	out := make([]CoreGameSeat, 0, len(r.Players))
	for _, p := range r.Players {
		out = append(out, CoreGameSeat{
			Subject:     p.Subject,
			TeamID:      p.TeamID,
			CharacterID: p.CharacterID,
			Incognito:   p.PlayerIdentity.Incognito,
		})
	}
	return out, nil
}

// Select sets (but does not lock) the agent for the match.
func (c *Client) Select(ctx context.Context, matchID, agentID string) error {
	return c.glz(ctx, "POST", c.glzURL("/pregame/v1/matches/"+matchID+"/select/"+agentID), nil)
}

// Lock locks the agent for the match (irreversible in-game).
func (c *Client) Lock(ctx context.Context, matchID, agentID string) error {
	return c.glz(ctx, "POST", c.glzURL("/pregame/v1/matches/"+matchID+"/lock/"+agentID), nil)
}

// OwnedAgents returns the UUIDs of agents the player owns (store entitlements).
func (c *Client) OwnedAgents(ctx context.Context) ([]string, error) {
	url := "https://" + c.auth.Endpoints.PDHost() + "/store/v1/entitlements/" + c.auth.PUUID + "/" + agentItemTypeID
	var r struct {
		Entitlements []struct {
			ItemID string `json:"ItemID"`
		} `json:"Entitlements"`
	}
	if err := c.glz(ctx, "GET", url, &r); err != nil {
		return nil, err
	}
	owned := make([]string, 0, len(r.Entitlements))
	for _, e := range r.Entitlements {
		owned = append(owned, e.ItemID)
	}
	return owned, nil
}

// ---- low-level helpers ---------------------------------------------------

func (c *Client) glzURL(path string) string {
	return "https://" + c.auth.Endpoints.GLZHost() + path
}

// pdURL builds a player-data (pd.{shard}.a.pvp.net) URL — MMR, match history,
// match details, name-service all live here (vs the live-session GLZ host).
func (c *Client) pdURL(path string) string {
	return "https://" + c.auth.Endpoints.PDHost() + path
}

func (c *Client) localGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://127.0.0.1:"+c.port+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.basic)
	return do(c.local, req, out)
}

// glz issues an authenticated GLZ/PD request, decoding into out (nil to discard).
func (c *Client) glz(ctx context.Context, method, url string, out any) error {
	return c.glzBody(ctx, method, url, nil, out)
}

// glzBody is glz with an optional JSON request body (name-service is a PUT).
func (c *Client) glzBody(ctx context.Context, method, url string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.auth.AccessToken)
	req.Header.Set("X-Riot-Entitlements-JWT", c.auth.EntitlementToken)
	req.Header.Set("X-Riot-ClientPlatform", platformHeaderB64)
	req.Header.Set("X-Riot-ClientVersion", c.clientVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return do(c.remote, req, out)
}

func (c *Client) fetchClientVersion(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://valorant-api.com/v1/version", nil)
	if err != nil {
		return fallbackClientVersion
	}
	var r struct {
		Data struct {
			RiotClientVersion string `json:"riotClientVersion"`
		} `json:"data"`
	}
	if err := do(c.remote, req, &r); err != nil || r.Data.RiotClientVersion == "" {
		return fallbackClientVersion
	}
	return r.Data.RiotClientVersion
}

func do(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode >= 400:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := strings.TrimSpace(string(body))
		// An expired/invalid RSO access token comes back as 400 BAD_CLAIMS
		// ("Failure validating/decoding RSO Access Token") — common after a
		// while in a match. Treat it as unauthorized so callers re-authenticate
		// from the local API (which holds a freshly refreshed token).
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(msg, "BAD_CLAIMS") {
			return ErrUnauthorized
		}
		return fmt.Errorf("riot %s %s: %d %s", req.Method, req.URL.Host, resp.StatusCode, msg)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
