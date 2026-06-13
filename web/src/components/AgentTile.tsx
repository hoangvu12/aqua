import { Ban, Check, Lock } from "lucide-react";
import type { Agent } from "@/lib/types";
import { cn } from "@/lib/utils";

export type TileReason = "not-owned" | "taken" | null;

/**
 * One agent in the grid. displayIcon only (perf). States: default, selected
 * (accent ring + check), and two disabled reasons with distinct glyphs:
 * not-owned (Lock) and taken/ally-locked (Ban).
 */
export function AgentTile({
  agent,
  selected,
  reason,
  frozen,
  onClick,
}: {
  agent: Agent;
  selected: boolean;
  reason: TileReason;
  frozen: boolean; // our own pick is locked → grid non-interactive
  onClick: () => void;
}) {
  const disabled = reason !== null || frozen;
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      aria-pressed={selected}
      aria-label={agent.displayName}
      className={cn(
        "group relative aspect-square overflow-hidden rounded-[var(--radius-tile)] border bg-surface transition-colors duration-150",
        selected ? "border-accent ring-2 ring-accent" : "border-hairline",
        !disabled && "active:bg-surface-hi",
        reason && "opacity-40 grayscale",
      )}
    >
      {agent.displayIcon && (
        <img
          src={agent.displayIcon}
          alt=""
          loading="lazy"
          draggable={false}
          className="h-full w-full object-cover"
        />
      )}

      {/* Name strip */}
      <span className="absolute inset-x-0 bottom-0 truncate bg-gradient-to-t from-bg/90 to-transparent px-1.5 pb-1 pt-3 text-[11px] font-semibold leading-none text-fg">
        {agent.displayName}
      </span>

      {selected && (
        <span className="absolute right-1 top-1 grid h-5 w-5 place-items-center rounded-full bg-accent text-on-accent">
          <Check className="h-3.5 w-3.5" strokeWidth={3} />
        </span>
      )}

      {reason && (
        <span className="absolute right-1 top-1 grid h-6 w-6 place-items-center rounded-full bg-bg/80 text-fg-dim">
          {reason === "not-owned" ? (
            <Lock className="h-3.5 w-3.5" />
          ) : (
            <Ban className="h-3.5 w-3.5 text-accent" />
          )}
        </span>
      )}
    </button>
  );
}
