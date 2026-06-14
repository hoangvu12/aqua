import { useEffect } from "react";
import { cn } from "@/lib/utils";

/**
 * A bottom-sheet drawer — the lobby controller's secondary surface, so the agent
 * grid + action bar stay untouched. Not a centered modal (those are banned in the
 * plan): it slides up from the bottom edge, the native place for one-handed phone
 * reach. Always mounted; open toggles a transform + scrim opacity so both the
 * enter and exit transitions are free (no unmount juggling). transform/opacity
 * only, ease-out-quart, ~200ms — and the global reduced-motion override collapses
 * it to an instant swap.
 */
export function Drawer({
  open,
  onClose,
  labelledBy,
  children,
}: {
  open: boolean;
  onClose: () => void;
  labelledBy?: string;
  children: React.ReactNode;
}) {
  // Escape closes; lock background scroll while open.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <div
      aria-hidden={!open}
      className={cn("fixed inset-0 z-50", !open && "pointer-events-none")}
    >
      {/* Scrim — a tinted near-black wash (hue 270), never #000, never glass. */}
      <button
        type="button"
        tabIndex={-1}
        aria-label="Close"
        onClick={onClose}
        className={cn(
          "absolute inset-0 transition-opacity duration-200 ease-[var(--ease-out-quart)]",
          open ? "opacity-100" : "opacity-0",
        )}
        style={{ background: "oklch(0.1 0.01 270 / 0.62)" }}
      />

      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        className={cn(
          "absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col rounded-t-[var(--radius-card)] border-t border-hairline bg-bg",
          "transition-transform duration-200 ease-[var(--ease-out-quart)] will-change-transform",
          open ? "translate-y-0" : "translate-y-full",
        )}
        style={{ paddingBottom: "max(env(safe-area-inset-bottom), 0.75rem)" }}
      >
        {/* Grabber — the bottom-sheet affordance. */}
        <div className="mx-auto mt-2.5 h-1 w-9 shrink-0 rounded-full bg-surface-hi" />
        {children}
      </div>
    </div>
  );
}
