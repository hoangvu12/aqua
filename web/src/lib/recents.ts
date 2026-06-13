// Phone-local memory of the last few agents the player committed to (locked or
// armed as a pre-pick). Surfaced as a one-tap "Recent" row above the roster so
// the most likely next pick is reachable without scrolling under the clock.

const KEY = "aqua_recent_agents";
const MAX = 3;

export function loadRecents(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return [];
    return arr.filter((x): x is string => typeof x === "string").slice(0, MAX);
  } catch {
    return [];
  }
}

/** Promote an agent to the front of the recents list (deduped, capped). */
export function pushRecent(uuid: string): string[] {
  if (!uuid) return loadRecents();
  const next = [uuid, ...loadRecents().filter((u) => u !== uuid)].slice(0, MAX);
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // best-effort; private-mode / quota failures are non-fatal
  }
  return next;
}
