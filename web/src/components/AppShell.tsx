import type { ReactNode } from "react";
import type { Agent, GameMap } from "@/lib/types";
import { agentGradient, cn } from "@/lib/utils";

/**
 * Responsive stage + frame (the desktop/tablet adaptation). On phones the app is
 * full-bleed and unchanged. From `md` up it becomes a centered, phone-width
 * controller floating on a dark stage, with the live agent portrait + gradient
 * (or the map splash) bleeding into the margins under a scrim. The controller
 * identity is kept; the empty desktop space turns into atmosphere from real game
 * art (PRODUCT.md principle 5) instead of invented decoration. The scoreboard
 * opts into a wider frame so it can read as a real table.
 */
export function AppShell({
  children,
  wide = false,
  agent,
  map,
}: {
  children: ReactNode;
  wide?: boolean;
  agent?: Agent;
  map?: GameMap;
}) {
  return (
    <div className="relative flex h-dvh flex-col overflow-hidden bg-bg md:items-center md:justify-center md:p-6 lg:p-10">
      <Stage agent={agent} map={map} />
      <div
        className={cn(
          "relative z-10 flex h-full w-full flex-col overflow-hidden bg-bg",
          "md:h-[min(920px,92dvh)] md:rounded-[26px] md:border md:border-hairline",
          "md:shadow-[0_40px_120px_-30px_rgba(0,0,0,0.85)]",
          wide ? "md:max-w-3xl" : "md:max-w-[26.5rem]",
        )}
      >
        {children}
      </div>
    </div>
  );
}

/** Full-viewport ambient backdrop, only painted from `md` up. */
function Stage({ agent, map }: { agent?: Agent; map?: GameMap }) {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0 hidden overflow-hidden md:block"
    >
      {map?.splash && (
        <img
          src={map.splash}
          alt=""
          className="absolute inset-0 h-full w-full scale-105 object-cover opacity-20 blur-sm"
        />
      )}

      {/* Agent gradient wash — recoloured the instant the selection changes. */}
      {agent && (
        <div
          key={`g-${agent.uuid}`}
          className="absolute inset-0 animate-[stage-in_600ms_var(--ease-out-quart)] opacity-45"
          style={{ background: agentGradient(agent.backgroundGradientColors, "145deg") }}
        />
      )}

      {/* Portrait bleeds in from the right margin, behind the frame. */}
      {agent?.fullPortrait && (
        <img
          key={`p-${agent.uuid}`}
          src={agent.fullPortrait}
          alt=""
          className="absolute -right-[6%] bottom-0 h-[94%] animate-[stage-in_600ms_var(--ease-out-quart)] object-contain opacity-30 lg:right-[1%]"
        />
      )}

      {/* Scrims: hold the chrome quiet, vignette toward the edges so the frame reads. */}
      <div className="absolute inset-0 bg-bg/65" />
      <div className="absolute inset-0 [background:radial-gradient(ellipse_at_center,transparent_25%,var(--color-bg)_82%)]" />
    </div>
  );
}
