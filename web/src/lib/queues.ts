// VALORANT matchmaking queue ids (the strings Riot's parties API expects) mapped
// to localized labels. Kept to the queues a party can actually start from the
// lobby; the order here is the order shown in the drawer's queue picker.

import { t, type Lang, type TKey } from "./i18n";

export interface QueueDef {
  /** Riot queueID sent to `party_set_queue`. */
  id: string;
  /** i18n key for the display label. */
  key: TKey;
}

export const QUEUES: QueueDef[] = [
  { id: "competitive", key: "qCompetitive" },
  { id: "unrated", key: "qUnrated" },
  { id: "swiftplay", key: "qSwiftplay" },
  { id: "spikerush", key: "qSpikeRush" },
  { id: "deathmatch", key: "qDeathmatch" },
  { id: "hurm", key: "qTeamDeathmatch" },
];

/** Localized label for a queue id; falls back to a title-cased raw id for queues
 * we don't list (e.g. a future mode), so the bar never shows an empty slot. */
export function queueLabel(lang: Lang, id: string): string {
  if (!id) return "";
  const def = QUEUES.find((q) => q.id === id);
  if (def) return t(lang, def.key);
  return id.charAt(0).toUpperCase() + id.slice(1);
}
