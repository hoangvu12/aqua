package picker

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
	// SelfAgentUUID/SelfStatus are the local player's own seat (game truth), so the
	// phone reflects picks made on the PC and renders correctly on cold-start.
	SelfAgentUUID string `json:"self_agent_uuid"`
	SelfStatus    string `json:"self_status"` // ""|selected|locked
}

// Teammate is one ally-team seat as shown in the allies strip. Self marks the
// local player's own seat so the phone can highlight it among the team.
type Teammate struct {
	Name      string `json:"name"`
	AgentUUID string `json:"agent_uuid"`
	Status    string `json:"status"` // ""|selected|locked
	Self      bool   `json:"self"`
}
