// Wire types — must mirror pc/internal/picker/state.go (the `state` frame the PC
// pushes through the relay) and the relay envelope from the Worker/DO.

/** Game phase / connection state. `offline` = game not running on the PC. */
export type GameState =
  | "offline"
  | "menus"
  | "lobby"
  | "queue"
  | "matchfound"
  | "pregame"
  | "locked"
  | "ingame"
  | "error";

/** Pre-pick lifecycle. `locking` is optimistic; `locked`/`taken` are game-derived. */
export type PrepickStatus = "none" | "armed" | "locking" | "locked" | "taken";

/** One ally-team seat in agent select. status ∈ ""|selected|locked. `self`
 * marks the local player's own seat so the strip can highlight it. */
export interface Teammate {
  name: string;
  agent_uuid: string;
  status: "" | "selected" | "locked";
  self: boolean;
}

/** The `state` object pushed by the PC. Fields are always present. */
export interface GameStateMsg {
  state: GameState;
  match_id: string;
  map_id: string;
  queue_id: string;
  prepick_agent_uuid: string;
  auto_lock: boolean;
  enabled: boolean;
  phase_time_remaining_ns: number;
  owned_agent_uuids: string[];
  taken_agent_uuids: string[];
  prepick_status: PrepickStatus;
  game_locale: string;
  teammates: Teammate[];
  /** The local player's own seat (game truth), so the phone reflects picks made
   * on the PC and renders correctly on cold-start. status ∈ ""|selected|locked. */
  self_agent_uuid: string;
  self_status: "" | "selected" | "locked";
}

/** Relay envelope: every WS frame is { type, reqId?, data }. */
export interface Frame<T = unknown> {
  type: string;
  reqId?: string;
  data?: T;
}

export interface ResultData {
  ok: boolean;
  message: string;
}

export interface AuthStatusData {
  ok: boolean;
  message: string;
}

// ── valorant-api.com catalog types (only the fields we use) ──────────────────

export interface Agent {
  uuid: string;
  displayName: string;
  displayIcon: string | null;
  fullPortrait: string | null;
  backgroundGradientColors: string[]; // 4× RRGGBBAA
  isPlayableCharacter: boolean;
  role: AgentRole | null;
}

export interface AgentRole {
  uuid: string;
  displayName: string;
  displayIcon: string | null;
}

export interface GameMap {
  uuid: string;
  displayName: string;
  splash: string | null;
  mapUrl: string; // GLZ MapID path — join key, NOT displayName
}

export interface Catalog {
  version: string;
  language: string;
  agents: Agent[];
  maps: GameMap[];
}
