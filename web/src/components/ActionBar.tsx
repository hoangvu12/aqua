import { useEffect, useRef, useState } from "react";
import { Check, Lock, ShieldAlert, X } from "lucide-react";
import type { Agent, GameStateMsg } from "@/lib/types";
import { t, type Lang } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";

/**
 * The morphing action surface, pinned to the bottom. Its shape follows the game
 * phase: arm a pre-pick before the match, tap-then-confirm lock during agent
 * select, and a settled "locked in" once the game confirms our lock.
 */
export function ActionBar({
  game,
  selectedAgent,
  lang,
  pendingLock,
  onLock,
  onArm,
  onDisarm,
  onToggleAutoLock,
}: {
  game: GameStateMsg;
  selectedAgent: Agent | undefined;
  lang: Lang;
  pendingLock: boolean;
  onLock: () => void;
  onArm: () => void;
  onDisarm: () => void;
  onToggleAutoLock: () => void;
}) {
  const isLocked = game.state === "locked" || game.self_status === "locked";
  const inPregame = game.state === "pregame" && !isLocked;
  const armed = !!game.prepick_agent_uuid;
  const taken = game.prepick_status === "taken";
  const locking = pendingLock || game.prepick_status === "locking";

  return (
    <footer className="border-t border-hairline bg-bg/95 px-4 pb-4 pt-3">
      {isLocked ? (
        <LockedRow agent={selectedAgent} lang={lang} />
      ) : inPregame ? (
        <PregameRow
          agent={selectedAgent}
          lang={lang}
          taken={taken}
          locking={locking}
          onLock={onLock}
        />
      ) : (
        <PrepareRow
          agent={selectedAgent}
          lang={lang}
          armed={armed}
          autoLock={game.auto_lock}
          onArm={onArm}
          onDisarm={onDisarm}
          onToggleAutoLock={onToggleAutoLock}
        />
      )}
    </footer>
  );
}

function AgentThumb({ agent }: { agent: Agent | undefined }) {
  if (!agent?.displayIcon) return <div className="h-11 w-11 rounded-[var(--radius-tile)] bg-surface" />;
  return (
    <img
      src={agent.displayIcon}
      alt=""
      className="h-11 w-11 rounded-[var(--radius-tile)] bg-surface object-cover"
    />
  );
}

function LockedRow({ agent, lang }: { agent: Agent | undefined; lang: Lang }) {
  return (
    <div className="flex items-center gap-3">
      <AgentThumb agent={agent} />
      <div className="min-w-0 flex-1">
        <p className="label text-[11px] text-ok">{t(lang, "lockedIn")}</p>
        <p className="truncate text-lg font-bold">{agent?.displayName ?? "—"}</p>
      </div>
      <span className="grid h-11 w-11 place-items-center rounded-full bg-ok/15 text-ok">
        <Check className="h-5 w-5" strokeWidth={3} />
      </span>
    </div>
  );
}

function PregameRow({
  agent,
  lang,
  taken,
  locking,
  onLock,
}: {
  agent: Agent | undefined;
  lang: Lang;
  taken: boolean;
  locking: boolean;
  onLock: () => void;
}) {
  if (taken) {
    return (
      <div className="flex items-center gap-3 rounded-[var(--radius-tile)] bg-accent/12 px-3 py-2.5">
        <ShieldAlert className="h-5 w-5 shrink-0 text-accent" />
        <p className="text-sm font-semibold text-fg">
          <span className="text-accent">{agent?.displayName ?? t(lang, "taken")}</span>{" "}
          {t(lang, "takenPickAnother")}
        </p>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-3">
      <AgentThumb agent={agent} />
      <div className="min-w-0 flex-1">
        <p className="label text-[11px] text-fg-mute">{t(lang, "state_pregame")}</p>
        <p className="truncate text-lg font-bold">{agent?.displayName ?? t(lang, "selectAgent")}</p>
      </div>
      <HoldLockButton agent={agent} lang={lang} locking={locking} onLock={onLock} />
    </div>
  );
}

/**
 * Hold-to-lock: the signature gesture (DESIGN.md §Motion). Press and hold ~600ms
 * and an accent fill sweeps left→right; at full it auto-commits the lock. Release
 * before it fills cancels cleanly. transform/opacity only, and reduced-motion
 * users simply get an instant fill (the global override) while the hold timer
 * still gates the action.
 */
const HOLD_MS = 600;

function HoldLockButton({
  agent,
  lang,
  locking,
  onLock,
}: {
  agent: Agent | undefined;
  lang: Lang;
  locking: boolean;
  onLock: () => void;
}) {
  const [holding, setHolding] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const disabled = !agent || locking;

  const clear = () => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  };
  const start = () => {
    if (disabled) return;
    setHolding(true);
    timer.current = setTimeout(() => {
      setHolding(false);
      onLock();
    }, HOLD_MS);
  };
  const cancel = () => {
    clear();
    setHolding(false);
  };
  useEffect(() => clear, []);

  return (
    <button
      type="button"
      disabled={disabled}
      onPointerDown={start}
      onPointerUp={cancel}
      onPointerLeave={cancel}
      onPointerCancel={cancel}
      onContextMenu={(e) => e.preventDefault()}
      aria-label={t(lang, "holdToLock")}
      style={{ touchAction: "none" }}
      className={cn(
        "relative h-14 min-w-32 select-none overflow-hidden rounded-[var(--radius-tile)] border text-base font-semibold outline-none transition-colors duration-150 focus-visible:ring-2 focus-visible:ring-accent",
        disabled
          ? "border-hairline bg-surface text-fg-mute opacity-45"
          : holding
            ? "border-accent text-on-accent"
            : "border-hairline bg-surface text-fg active:bg-surface-hi md:hover:bg-surface-hi",
      )}
    >
      {/* Progress fill: sweeps over the hold, snaps back on release. */}
      <span
        aria-hidden
        className="absolute inset-0 origin-left bg-accent ease-[var(--ease-out-quart)]"
        style={{
          transform: holding ? "scaleX(1)" : "scaleX(0)",
          transitionProperty: "transform",
          transitionDuration: holding ? `${HOLD_MS}ms` : "150ms",
        }}
      />
      <span className="relative z-10 inline-flex items-center justify-center gap-2">
        <Lock className="h-4 w-4" />
        {locking ? t(lang, "locking") : t(lang, "holdToLock")}
      </span>
    </button>
  );
}

function PrepareRow({
  agent,
  lang,
  armed,
  autoLock,
  onArm,
  onDisarm,
  onToggleAutoLock,
}: {
  agent: Agent | undefined;
  lang: Lang;
  armed: boolean;
  autoLock: boolean;
  onArm: () => void;
  onDisarm: () => void;
  onToggleAutoLock: () => void;
}) {
  return (
    <div className="space-y-3">
      {/* Auto-lock toggle + ban-risk honesty (PRODUCT.md principle 4). */}
      <button
        onClick={onToggleAutoLock}
        className="flex w-full items-center gap-3 text-left"
        role="switch"
        aria-checked={autoLock}
      >
        <Toggle on={autoLock} />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold">{t(lang, "autoLock")}</p>
          <p className="text-[11px] leading-tight text-fg-mute">{t(lang, "banWarning")}</p>
        </div>
      </button>

      <div className="flex items-center gap-3">
        <AgentThumb agent={agent} />
        <div className="min-w-0 flex-1">
          <p className="label text-[11px] text-fg-mute">
            {armed ? t(lang, "prepickArmed") : t(lang, "armPrepick")}
          </p>
          <p className="truncate text-lg font-bold">
            {agent?.displayName ?? t(lang, "pickAnAgent")}
          </p>
        </div>
        {armed ? (
          <Button variant="surface" size="lg" onClick={onDisarm}>
            <X className="h-4 w-4" />
            {t(lang, "disarm")}
          </Button>
        ) : (
          <Button variant="accent" size="lg" disabled={!agent} onClick={onArm} className="min-w-28">
            {t(lang, "armPrepick")}
          </Button>
        )}
      </div>
    </div>
  );
}

function Toggle({ on }: { on: boolean }) {
  return (
    <span
      className={cn(
        "relative h-7 w-12 shrink-0 rounded-full transition-colors duration-150",
        on ? "bg-accent" : "bg-surface-hi",
      )}
    >
      <span
        className={cn(
          "absolute top-0.5 h-6 w-6 rounded-full bg-on-accent transition-transform duration-150 ease-[var(--ease-out-quart)]",
          on ? "translate-x-[22px]" : "translate-x-0.5",
        )}
      />
    </span>
  );
}
