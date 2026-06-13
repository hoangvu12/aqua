import { useMemo } from "react";
import { History } from "lucide-react";
import type { Agent, GameStateMsg } from "@/lib/types";
import { t, type Lang } from "@/lib/i18n";
import { AgentTile, type TileReason } from "./AgentTile";

/**
 * 3-column agent grid. Computes each tile's reason from game truth: not-owned
 * (outside owned_agent_uuids) or taken (ally-locked). The grid freezes once our
 * own pick is locked. Owned filtering is intentionally NOT a hard hide so the
 * player still sees the full roster, just gated. When browsing the full roster
 * (no role filter), a "Recent" row pins the last picks one tap above the scroll.
 */
export function AgentGrid({
  agents,
  game,
  roleFilter,
  recents,
  selectedUuid,
  onSelect,
  lang,
}: {
  agents: Agent[];
  game: GameStateMsg;
  roleFilter: string | null;
  recents: string[];
  selectedUuid: string | null;
  onSelect: (uuid: string) => void;
  lang: Lang;
}) {
  const owned = useMemo(() => new Set(game.owned_agent_uuids), [game.owned_agent_uuids]);
  const taken = useMemo(() => new Set(game.taken_agent_uuids), [game.taken_agent_uuids]);
  const frozen = game.self_status === "locked";
  const byUuid = useMemo(() => new Map(agents.map((a) => [a.uuid, a])), [agents]);

  const reasonFor = (agent: Agent): TileReason => {
    if (owned.size > 0 && !owned.has(agent.uuid)) return "not-owned";
    if (taken.has(agent.uuid) && agent.uuid !== selectedUuid) return "taken";
    return null;
  };

  const renderTile = (agent: Agent) => (
    <AgentTile
      key={agent.uuid}
      agent={agent}
      selected={selectedUuid === agent.uuid}
      reason={reasonFor(agent)}
      frozen={frozen}
      onClick={() => onSelect(agent.uuid)}
    />
  );

  const shown = useMemo(
    () => agents.filter((a) => (roleFilter ? a.role?.uuid === roleFilter : true)),
    [agents, roleFilter],
  );

  // Resolve recents against the live catalog so retired/stale ids fall away.
  const recentAgents = useMemo(
    () => (roleFilter ? [] : recents.map((u) => byUuid.get(u)).filter((a): a is Agent => !!a)),
    [roleFilter, recents, byUuid],
  );

  return (
    <div className="px-4 pb-4 pt-1">
      {recentAgents.length > 0 && (
        <div className="mb-3">
          <p className="label mb-2 flex items-center gap-1.5 text-[11px] text-fg-mute">
            <History className="h-3 w-3" />
            {t(lang, "recent")}
          </p>
          <div className="grid grid-cols-5 gap-2">{recentAgents.map(renderTile)}</div>
          <div className="mt-3 border-t border-hairline" />
        </div>
      )}
      <div className="grid grid-cols-5 gap-2">{shown.map(renderTile)}</div>
    </div>
  );
}
