import type { GameStateMsg } from "./types";

/**
 * The optimistic layer for the React Query cache. The phone's commands round-trip
 * phone→PC→VALORANT→poll before a fresh `state` frame echoes them back, so without
 * this every tap visibly lags (and tempts a frustrated double-tap).
 *
 * React Query gives us the mutation lifecycle (onMutate applies the guess to the
 * cache, onError rolls it back), but it has no concept of "hold this guess until
 * the pushed truth catches up" — and the frame that arrives right after a command
 * is usually *stale* (the PC polled before the change landed). So we keep a small
 * registry of in-flight Patches and re-apply the still-unconfirmed ones on top of
 * every incoming frame (see `overlay`). That is the custom `reconcile` step the
 * push model forces; everything else is stock React Query.
 */
export interface Patch {
  /** Identity — a newer patch on the same key replaces the older one. */
  key: string;
  /** Overlay the predicted change onto a state value. */
  apply: (g: GameStateMsg) => GameStateMsg;
  /** True once the pushed truth reflects the change → the patch can retire. */
  settled: (g: GameStateMsg) => boolean;
}

/** A patch that sets one field and settles once the truth carries that value. */
export function fieldPatch<K extends keyof GameStateMsg>(field: K, value: GameStateMsg[K]): Patch {
  return {
    key: `field:${String(field)}`,
    apply: (g) => (g[field] === value ? g : { ...g, [field]: value }),
    settled: (g) => g[field] === value,
  };
}

/**
 * Apply every still-unconfirmed patch over the freshly pushed truth, and report
 * which patches the truth has now caught up to so the caller can retire them. A
 * settled patch is dropped (no longer applied), giving a flicker-free handoff from
 * guess to truth; the rest stay overlaid until the next frame confirms them.
 */
export function overlay(
  truth: GameStateMsg | null,
  patches: Iterable<Patch>,
): { view: GameStateMsg | null; settled: string[] } {
  if (!truth) return { view: truth, settled: [] };
  const settled: string[] = [];
  let view = truth;
  for (const p of patches) {
    if (p.settled(truth)) settled.push(p.key);
    else view = p.apply(view);
  }
  return { view, settled };
}
