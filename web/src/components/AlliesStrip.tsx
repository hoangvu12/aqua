import { useMemo, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import type { Catalog, Teammate } from "@/lib/types";
import { agentByUuid } from "@/lib/catalog";
import { t, type Lang } from "@/lib/i18n";
import { cn } from "@/lib/utils";

/**
 * Read-only team awareness. Collapsed by default to a single count line
 * (locked · picking · open) so it never competes with the roster mid-pick; tap
 * to expand into one seat per ally. Seats carry three distinct affordances:
 * locked (solid ring + check, full opacity), selected/hovering (dashed ring,
 * dimmed), and empty/late-join (dashed hairline, placeholder dot). The local
 * player's own seat sorts first and is accent-tinted so it reads as "you".
 *
 * Glyph contract (shared across the app): Check = committed/locked-in, Lock =
 * not-owned, Ban = taken. Allies use Check for locked seats to match the action
 * bar's locked-in state.
 */
export function AlliesStrip({
  teammates,
  catalog,
  lang,
}: {
  teammates: Teammate[];
  catalog: Catalog | null;
  lang: Lang;
}) {
  const [expanded, setExpanded] = useState(false);

  // Self first, then keep the game's order for the rest (stable).
  const ordered = useMemo(
    () => teammates.map((tm, i) => ({ tm, i })).sort((a, b) => Number(b.tm.self) - Number(a.tm.self)),
    [teammates],
  );

  const counts = useMemo(() => {
    let locked = 0;
    let picking = 0;
    let open = 0;
    for (const tm of teammates) {
      if (tm.status === "locked") locked++;
      else if (tm.status === "selected") picking++;
      else open++;
    }
    return { locked, picking, open };
  }, [teammates]);

  if (teammates.length === 0) return null;

  return (
    <div className="border-t border-hairline px-4 py-2.5">
      <button
        onClick={() => setExpanded((v) => !v)}
        className="group flex w-full items-center gap-3 text-left"
        aria-expanded={expanded}
      >
        <span className="label text-[11px] text-fg-mute">{t(lang, "allies")}</span>
        <span className="flex flex-1 items-center gap-3 text-xs font-medium text-fg-mute">
          {counts.locked > 0 && <Count color="text-ok" n={counts.locked} label={t(lang, "aLocked")} />}
          {counts.picking > 0 && (
            <Count color="text-fg-dim" n={counts.picking} label={t(lang, "aPicking")} />
          )}
          {counts.open > 0 && <Count color="text-fg-mute" n={counts.open} label={t(lang, "aOpen")} />}
        </span>
        <ChevronDown
          className={cn(
            "h-4 w-4 shrink-0 text-fg-mute transition-[transform,color] duration-150 ease-[var(--ease-out-quart)] md:group-hover:text-fg-dim",
            expanded && "rotate-180",
          )}
        />
      </button>

      {expanded && (
        <div className="mt-2.5 flex gap-2.5">
          {ordered.map(({ tm, i }) => (
            <Seat key={`${tm.name}-${i}`} tm={tm} catalog={catalog} lang={lang} />
          ))}
        </div>
      )}
    </div>
  );
}

function Count({ color, n, label }: { color: string; n: number; label: string }) {
  return (
    <span className="inline-flex items-baseline gap-1">
      <span className={cn("font-semibold tabular-nums", color)}>{n}</span>
      {label}
    </span>
  );
}

function Seat({ tm, catalog, lang }: { tm: Teammate; catalog: Catalog | null; lang: Lang }) {
  const agent = tm.agent_uuid ? agentByUuid(catalog, tm.agent_uuid) : undefined;
  const locked = tm.status === "locked";
  const selected = tm.status === "selected";

  return (
    <div className="flex min-w-0 flex-col items-center gap-1">
      {/* Non-clipping wrapper: the avatar clips the art to a circle, but the status
          badge is a sibling layered on top so its overhang is never cut off. */}
      <div className="relative">
        <div
          className={cn(
            "grid h-11 w-11 place-items-center overflow-hidden rounded-full border bg-surface",
            // Ring: self is always accent-tinted; allies use neutral tiers.
            tm.self
              ? locked
                ? "border-2 border-accent"
                : "border-dashed border-accent/55"
              : locked
                ? "border-fg-mute"
                : "border-dashed border-hairline",
            // Opacity by certainty: locked = committed, selected = tentative, empty = absent.
            locked ? "opacity-100" : selected ? "opacity-75" : "opacity-50",
          )}
        >
          {agent?.displayIcon ? (
            <img
              src={agent.displayIcon}
              alt={agent.displayName}
              className={cn("h-full w-full object-cover", !locked && "grayscale-[0.3]")}
            />
          ) : (
            <span className="h-2 w-2 rounded-full bg-fg-mute" />
          )}
        </div>

        {/* Locked → check badge (committed). Self uses the accent; allies stay
            neutral. ring-2 ring-bg lifts it off the avatar edge. */}
        {locked && (
          <span
            className={cn(
              "absolute -bottom-0.5 -right-0.5 grid h-4 w-4 place-items-center rounded-full ring-2 ring-bg",
              tm.self ? "bg-accent text-on-accent" : "bg-surface-hi text-fg-dim",
            )}
          >
            <Check className="h-2.5 w-2.5" strokeWidth={3} />
          </span>
        )}

        {/* Selecting/hovering → a small static dot signals "still choosing". */}
        {selected && (
          <span className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full bg-fg-dim ring-2 ring-bg" />
        )}
      </div>

      <span
        className={cn(
          "max-w-12 truncate text-[10px]",
          tm.self ? "font-semibold text-accent" : "text-fg-mute",
        )}
      >
        {tm.self ? t(lang, "you") : tm.name || t(lang, "empty")}
      </span>
    </div>
  );
}
