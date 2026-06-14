package picker

import "aqua/internal/riot"

// State is the wire `state` object pushed to phones (see plan §Relay protocol).
// Fields are always present (no omitempty) so the phone renders a stable shape.
type State struct {
	State                string     `json:"state"`
	MatchID              string     `json:"match_id"`
	MapID                string     `json:"map_id"`
	QueueID              string     `json:"queue_id"`
	PrepickAgentUUID     string     `json:"prepick_agent_uuid"`
	AutoLock             bool       `json:"auto_lock"`
	Enabled              bool       `json:"enabled"`
	PhaseTimeRemainingNS int64      `json:"phase_time_remaining_ns"`
	OwnedAgentUUIDs      []string   `json:"owned_agent_uuids"`
	TakenAgentUUIDs      []string   `json:"taken_agent_uuids"`
	PrepickStatus        string     `json:"prepick_status"`
	GameLocale           string     `json:"game_locale"`
	Teammates            []Teammate `json:"teammates"`
	// MatchPlayers is the live-match scoreboard (both teams), populated only in
	// the ingame state. Always present (never null) for a stable phone shape.
	MatchPlayers []MatchSeat `json:"match_players"`
	// SelfAgentUUID/SelfStatus are the local player's own seat (game truth), so the
	// phone reflects picks made on the PC and renders correctly on cold-start.
	SelfAgentUUID string `json:"self_agent_uuid"`
	SelfStatus    string `json:"self_status"` // ""|selected|locked

	// Party (lobby) surface — populated in the pre-match states (menus|lobby|
	// queue|matchfound), empty otherwise. Drives the phone's party drawer.
	PartyID            string        `json:"party_id"`
	PartyAccessibility string        `json:"party_accessibility"` // OPEN|CLOSED|""
	PartyInviteCode    string        `json:"party_invite_code"`
	PartyMaxSize       int           `json:"party_max_size"`
	IsPartyOwner       bool          `json:"is_party_owner"`
	QueueEntryTime     int64         `json:"queue_entry_time"` // unix millis, 0 when not queuing
	PartyMembers       []PartyMember `json:"party_members"`
}

// PartyMember is one seat in the pre-match party (the wire view). Self marks the
// local player; IsOwner marks the seat that can run owner-only actions. PUUID is
// the kick target (owner-only; safe to expose in the owner's own party).
type PartyMember struct {
	PUUID   string            `json:"puuid"`
	Name    string            `json:"name"`
	IsOwner bool              `json:"is_owner"`
	IsReady bool              `json:"is_ready"`
	Self    bool              `json:"self"`
	Stats   *riot.PlayerStats `json:"stats,omitempty"`
}

// Teammate is one ally-team seat as shown in the allies strip. Self marks the
// local player's own seat so the phone can highlight it among the team.
type Teammate struct {
	Name       string            `json:"name"`
	AgentUUID  string            `json:"agent_uuid"`
	Status     string            `json:"status"` // ""|selected|locked
	Self       bool              `json:"self"`
	Stats      *riot.PlayerStats `json:"stats,omitempty"` // tracker row; absent until fetched
	PartyGroup int               `json:"party_group"`     // 0 = none; 1..n = inferred premade group
}

// MatchSeat is one player row in the live-match scoreboard. Unlike Teammate it
// spans both teams (Team ∈ ally|enemy) and carries no agent-select status.
type MatchSeat struct {
	Name       string            `json:"name"`
	AgentUUID  string            `json:"agent_uuid"`
	Team       string            `json:"team"` // "ally"|"enemy"
	Self       bool              `json:"self"`
	Stats      *riot.PlayerStats `json:"stats,omitempty"`
	PartyGroup int               `json:"party_group"` // 0 = none; 1..n = inferred premade group
}
