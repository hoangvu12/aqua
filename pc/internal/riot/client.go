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

// defaultAgentIDs are the free starter agents every account can play. The store
// entitlements endpoint only lists *acquired* agents, so these five never appear
// there — we union them in so the phone doesn't show them as locked/not-owned.
// Brimstone, Jett, Phoenix, Sage, Sova.
var defaultAgentIDs = []string{
	"9f0d8ba9-4140-b941-57d3-a7ad57c6b417", // Brimstone
	"add6443a-41bd-e414-f6ad-e58d267f4e95", // Jett
	"eb93336a-449b-9c1b-0a54-a891f7921d69", // Phoenix
	"569fdd95-4d10-43ab-ca70-79becc718b46", // Sage
	"320b2a48-4d9b-a075-30f1-1f93a9b638fa", // Sova
}

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
// matchfound apart (the GLZ pregame/core-game checks only cover pregame/ingame),
// plus the lobby-management surface the phone drives (accessibility, invite code,
// members + who owns the party).
type PartyInfo struct {
	ID            string // party id (echoed; the caller already has it)
	State         string // DEFAULT|MATCHMAKING|MATCHMADE|CUSTOM_GAME_SETUP|…
	QueueID       string // selected queue (empty in the bare main menu)
	ReadyCheck    string // ""|None|InProgress (InProgress = match found, ready-check up)
	Accessibility string // OPEN|CLOSED
	InviteCode    string // "" when no code is active
	MaxMembers    int    // party size cap (MaxPartySize)
	// QueueEntryMillis is when matchmaking started (unix millis), for the phone's
	// search timer. 0 when not queuing (Riot sends a zero time outside the queue).
	QueueEntryMillis int64
	Members          []PartyMember
}

// PartyMember is one seat in the pre-match party (distinct from a pregame
// Teammate: there's no agent here, but there is ownership + ready state).
type PartyMember struct {
	PUUID     string
	IsOwner   bool
	IsReady   bool
	Tier      int  // current competitive tier (0 = unranked)
	Incognito bool // streamer mode — honor by redacting name
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

// Party fetches a party's matchmaking + ready-check state plus the lobby surface
// (accessibility, invite code, members). Undocumented and patch-fragile (the
// ReadyCheck path especially) → callers parse defensively and degrade to plain
// "menus" on any failure.
func (c *Client) Party(ctx context.Context, id string) (PartyInfo, error) {
	var r struct {
		ID              string `json:"ID"`
		State           string `json:"State"`
		Accessibility   string `json:"Accessibility"`
		InviteCode      string `json:"InviteCode"`
		MaxPartySize    int    `json:"MaxPartySize"`
		QueueEntryTime  string `json:"QueueEntryTime"` // ISO 8601; zero-time when idle
		MatchmakingData struct {
			QueueID    string `json:"QueueID"`
			ReadyCheck struct {
				State string `json:"State"`
			} `json:"ReadyCheck"`
		} `json:"MatchmakingData"`
		Members []struct {
			Subject         string `json:"Subject"`
			IsOwner         bool   `json:"IsOwner"`
			IsReady         bool   `json:"IsReady"`
			CompetitiveTier int    `json:"CompetitiveTier"`
			PlayerIdentity  struct {
				Incognito bool `json:"Incognito"`
			} `json:"PlayerIdentity"`
		} `json:"Members"`
	}
	if err := c.glz(ctx, "GET", c.glzURL("/parties/v1/parties/"+id), &r); err != nil {
		return PartyInfo{}, err
	}
	pi := PartyInfo{
		ID:            r.ID,
		State:         r.State,
		QueueID:       r.MatchmakingData.QueueID,
		ReadyCheck:    r.MatchmakingData.ReadyCheck.State,
		Accessibility: r.Accessibility,
		InviteCode:    r.InviteCode,
		MaxMembers:    r.MaxPartySize,
	}
	// Riot sends a zero time ("0001-01-01T…") when not queuing — guard on the year.
	if t, err := time.Parse(time.RFC3339, r.QueueEntryTime); err == nil && t.Year() > 1 {
		pi.QueueEntryMillis = t.UnixMilli()
	}
	for _, m := range r.Members {
		pi.Members = append(pi.Members, PartyMember{
			PUUID:     m.Subject,
			IsOwner:   m.IsOwner,
			IsReady:   m.IsReady,
			Tier:      m.CompetitiveTier,
			Incognito: m.PlayerIdentity.Incognito,
		})
	}
	return pi, nil
}

// ---- party management (GLZ) ----------------------------------------------
//
// The lobby surface the phone drives. These mirror normal Riot-client actions
// (not the instalock select/lock), so the ban posture is far lighter — but they
// are still automation. Each returns just an error; callers trigger a re-poll so
// the next pushed state reflects the change (the Party read above is the truth).
// Owner-only operations (matchmaking, queue, accessibility, code, kick) are gated
// by the caller against the owner flag; Riot also rejects them server-side.

// GenerateInviteCode (re)generates the party's 6-char invite code.
func (c *Client) GenerateInviteCode(ctx context.Context, id string) error {
	return c.glz(ctx, "POST", c.glzURL("/parties/v1/parties/"+id+"/invitecode"), nil)
}

// DisableInviteCode clears the party's invite code.
func (c *Client) DisableInviteCode(ctx context.Context, id string) error {
	return c.glz(ctx, "DELETE", c.glzURL("/parties/v1/parties/"+id+"/invitecode"), nil)
}

// JoinByCode joins the party that owns code (any player; not owner-gated).
func (c *Client) JoinByCode(ctx context.Context, code string) error {
	return c.glz(ctx, "POST", c.glzURL("/parties/v1/players/joinbycode/"+code), nil)
}

// LeaveParty removes the local player from their party (Riot re-creates a fresh
// solo party). DELETE on our own puuid; any player can leave.
func (c *Client) LeaveParty(ctx context.Context) error {
	return c.glz(ctx, "DELETE", c.glzURL("/parties/v1/players/"+c.auth.PUUID), nil)
}

// KickMember removes another player from the party (owner-only). Same endpoint as
// LeaveParty, just a different subject.
func (c *Client) KickMember(ctx context.Context, puuid string) error {
	return c.glz(ctx, "DELETE", c.glzURL("/parties/v1/players/"+puuid), nil)
}

// SetAccessibility flips the party between OPEN (anyone can join) and CLOSED.
func (c *Client) SetAccessibility(ctx context.Context, id string, open bool) error {
	acc := "CLOSED"
	if open {
		acc = "OPEN"
	}
	return c.glzBody(ctx, "POST", c.glzURL("/parties/v1/parties/"+id+"/accessibility"),
		map[string]string{"accessibility": acc}, nil)
}

// ChangeQueue sets the matchmaking queue (e.g. "competitive", "unrated").
func (c *Client) ChangeQueue(ctx context.Context, id, queueID string) error {
	return c.glzBody(ctx, "POST", c.glzURL("/parties/v1/parties/"+id+"/queue"),
		map[string]string{"queueID": queueID}, nil)
}

// StartMatchmaking enters the selected queue (owner-only).
func (c *Client) StartMatchmaking(ctx context.Context, id string) error {
	return c.glz(ctx, "POST", c.glzURL("/parties/v1/parties/"+id+"/matchmaking/join"), nil)
}

// StopMatchmaking leaves the queue (owner-only).
func (c *Client) StopMatchmaking(ctx context.Context, id string) error {
	return c.glz(ctx, "POST", c.glzURL("/parties/v1/parties/"+id+"/matchmaking/leave"), nil)
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

// OwnedAgents returns the UUIDs of agents the player can play: the free starter
// agents (always available) unioned with the store entitlements (acquired ones).
// The entitlements endpoint omits the defaults, so they're added explicitly.
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
	owned := make([]string, 0, len(defaultAgentIDs)+len(r.Entitlements))
	seen := make(map[string]bool, len(defaultAgentIDs)+len(r.Entitlements))
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			owned = append(owned, id)
		}
	}
	for _, id := range defaultAgentIDs {
		add(id)
	}
	for _, e := range r.Entitlements {
		add(e.ItemID)
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
