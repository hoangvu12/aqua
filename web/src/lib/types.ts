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

/** Tracker row for one player: identity, rank, and recent-form aggregates.
 * Mirrors riot.PlayerStats (pc/internal/riot/stats.go). Absent until the PC's
 * background fetch resolves it. */
export interface PlayerStats {
  puuid: string;
  name: string; // "GameName#TagLine"
  tier: number; // current competitive tier (0 = unranked)
  rr: number; // ranked rating within the tier
  peak_tier: number; // highest tier ever won a game at
  matches: number;
  wins: number;
  win_pct: number; // 0..100
  kd: number;
  adr: number; // avg damage / round
  hs_pct: number; // 0..100
  recent: RecentMatch[]; // newest-first recent form
}

/** One recent match in the form streak. `rr` is the RR gained/lost — present only
 * for competitive matches (null otherwise), so a dot can render as a +/- delta. */
export interface RecentMatch {
  won: boolean;
  rr: number | null;
}

/** One ally-team seat in agent select. status ∈ ""|selected|locked. `self`
 * marks the local player's own seat so the strip can highlight it. */
export interface Teammate {
  name: string;
  agent_uuid: string;
  status: "" | "selected" | "locked";
  self: boolean;
  stats?: PlayerStats | null;
  /** Inferred premade group: 0 = none, 1..n = a detected party (per match). */
  party_group: number;
}

/** Party accessibility: OPEN = anyone with the code joins, CLOSED = invite only.
 * "" before the party state resolves. */
export type PartyAccessibility = "OPEN" | "CLOSED" | "";

/** One seat in the pre-match party (lobby). Distinct from Teammate: no agent, but
 * carries ownership + ready state. `puuid` is the kick target. */
export interface PartyMember {
  puuid: string;
  name: string;
  is_owner: boolean;
  is_ready: boolean;
  self: boolean;
  stats?: PlayerStats | null;
}

/** One equipped skin shown in the scoreboard's expanded row. */
export interface SeatSkin {
  weapon: string; // "Vandal", "Knife", …
  name: string; // "Prelude to Chaos Vandal"
  image: string; // valorant-api render URL (rewritten to /cdn in prod)
}

/** One row in the live-match scoreboard (both teams). */
export interface MatchSeat {
  name: string;
  agent_uuid: string;
  team: "ally" | "enemy";
  self: boolean;
  stats?: PlayerStats | null;
  /** Inferred premade group: 0 = none, 1..n = a detected party (per match). */
  party_group: number;
  /** Equipped skins for a curated gun set; absent until the PC's loadout fetch
   * resolves, empty when the player runs all default skins. */
  skins?: SeatSkin[] | null;
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
  /** Live-match scoreboard (both teams); populated only in the `ingame` state. */
  match_players: MatchSeat[];
  /** Live round score (ingame only), from the PC's presence read. `score_ally` is
   * your team's rounds. `score_valid` gates rendering (0-0 is a real pistol
   * round, so a zero value alone isn't "unknown"). */
  score_ally: number;
  score_enemy: number;
  score_valid: boolean;
  /** The local player's own seat (game truth), so the phone reflects picks made
   * on the PC and renders correctly on cold-start. status ∈ ""|selected|locked. */
  self_agent_uuid: string;
  self_status: "" | "selected" | "locked";
  /** Party (lobby) surface; populated only in pre-match states (menus|lobby|
   * queue|matchfound). Drives the party drawer. */
  party_id: string;
  party_accessibility: PartyAccessibility;
  party_invite_code: string;
  party_max_size: number;
  is_party_owner: boolean;
  /** When matchmaking started (unix millis); 0 when not queuing. Drives the
   * search timer. */
  queue_entry_time: number;
  party_members: PartyMember[];
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

/** One competitive rank tier (valorant-api competitivetiers). `tier` is the
 * join key matched against PlayerStats.tier / peak_tier. */
export interface CompetitiveTier {
  tier: number;
  tierName: string; // e.g. "DIAMOND 2" (localized); "UNRANKED" for 0
  smallIcon: string | null;
  largeIcon: string | null;
}

export interface Catalog {
  version: string;
  language: string;
  agents: Agent[];
  maps: GameMap[];
  ranks: CompetitiveTier[];
}
