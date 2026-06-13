import { useEffect, useState } from "react";
import type { ConnStatus } from "@/lib/relay";
import type { Catalog, GameStateMsg } from "@/lib/types";
import { mapByMapId } from "@/lib/catalog";
import { t, type Lang } from "@/lib/i18n";
import { cn, formatCountdown } from "@/lib/utils";
import { ConnectionChip } from "./ConnectionChip";

const QUEUE_LABELS: Record<string, string> = {
  competitive: "Competitive",
  unrated: "Unrated",
  swiftplay: "Swiftplay",
  spikerush: "Spike Rush",
  deathmatch: "Deathmatch",
  ggteam: "Escalation",
  hurm: "Team Deathmatch",
  onefa: "Replication",
  newmap: "New Map",
};

function gameStateLabel(lang: Lang, s: GameStateMsg["state"]): string {
  const key = `state_${s}` as const;
  return t(lang, key);
}

/** Local 1Hz countdown that re-seeds whenever the PC pushes a fresh time value. */
function useCountdown(ns: number, active: boolean): number {
  const [remaining, setRemaining] = useState(ns);
  useEffect(() => {
    setRemaining(ns);
    if (!active || ns <= 0) return;
    const start = performance.now();
    const id = setInterval(() => {
      const elapsed = (performance.now() - start) * 1_000_000; // ms→ns
      setRemaining(Math.max(0, ns - elapsed));
    }, 250);
    return () => clearInterval(id);
  }, [ns, active]);
  return remaining;
}

export function StatusBar({
  conn,
  game,
  catalog,
  lang,
  onToggleLang,
}: {
  conn: ConnStatus;
  game: GameStateMsg | null;
  catalog: Catalog | null;
  lang: Lang;
  onToggleLang: () => void;
}) {
  const inSelect = game?.state === "pregame" || game?.state === "locked";
  const map = inSelect ? mapByMapId(catalog, game?.map_id ?? "") : undefined;
  const remaining = useCountdown(game?.phase_time_remaining_ns ?? 0, game?.state === "pregame");

  const queue = game?.queue_id ? (QUEUE_LABELS[game.queue_id] ?? game.queue_id) : "";
  // Urgency once the agent-select timer runs low (≤10s): pulse the countdown.
  const urgent = game?.state === "pregame" && remaining > 0 && remaining <= 10_000_000_000;

  return (
    <header className="relative overflow-hidden border-b border-hairline">
      {/* Map splash sits behind the status zone, but never during pregame: a red
          countdown over red art muddies the one readout that must dominate then.
          Kept for the settled `locked` state, where the decision is already made. */}
      {map?.splash && game?.state !== "pregame" && (
        <>
          <img
            src={map.splash}
            alt=""
            aria-hidden
            className="pointer-events-none absolute inset-0 h-full w-full object-cover opacity-40"
          />
          <div className="pointer-events-none absolute inset-0 bg-gradient-to-b from-bg/70 via-bg/85 to-bg" />
        </>
      )}

      <div className="relative px-4 pb-2 pt-1.5">
        <div className="flex items-center justify-between">
          <ConnectionChip conn={conn} lang={lang} />
          <button
            onClick={onToggleLang}
            className="rounded-full border border-hairline px-2.5 py-0.5 text-xs font-semibold text-fg-dim active:bg-surface"
          >
            {lang === "vi" ? "VI" : "EN"}
          </button>
        </div>

        <div className="mt-1 flex items-baseline justify-between gap-3">
          <div className="flex min-w-0 items-baseline gap-2">
            <h1 className="text-lg font-bold leading-tight tracking-tight">
              {game ? gameStateLabel(lang, game.state) : t(lang, "loading")}
            </h1>
            {(map || queue) && (
              <p className="truncate text-xs text-fg-mute">
                {map?.displayName}
                {map && queue ? " · " : ""}
                {queue}
              </p>
            )}
          </div>
          {game?.state === "pregame" && (
            <span
              className={cn(
                "shrink-0 tabular-nums text-2xl font-bold leading-none text-accent",
                urgent && "animate-pulse text-accent-hi",
              )}
            >
              {formatCountdown(remaining)}
            </span>
          )}
        </div>
      </div>
    </header>
  );
}
