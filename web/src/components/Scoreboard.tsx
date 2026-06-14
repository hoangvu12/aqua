import { useState } from "react";
import { ChevronDown, Swords } from "lucide-react";
import type { Agent, Catalog, CompetitiveTier, MatchSeat, RecentMatch } from "@/lib/types";
import { agentByUuid, rankByTier } from "@/lib/catalog";
import { t, type Lang } from "@/lib/i18n";
import { cn } from "@/lib/utils";

/**
 * Live-match scoreboard (the `ingame` state, replacing the agent grid). Two
 * teams, one tap-to-expand row per player. The card itself carries everything
 * glanceable: agent portrait, name, current + peak rank, headline K/D, recent
 * W/L. Tapping reveals the fuller stat line (ADR / HS% / Win% / matches).
 */
export function Scoreboard({
  players,
  score,
  catalog,
  lang,
}: {
  players: MatchSeat[];
  /** Live round score (your team : enemy), or null when the PC couldn't read it. */
  score: { ally: number; enemy: number } | null;
  catalog: Catalog | null;
  lang: Lang;
}) {
  const ally = players.filter((p) => p.team === "ally");
  const enemy = players.filter((p) => p.team === "enemy");

  return (
    <div className="no-scrollbar flex-1 overflow-y-auto px-3 py-2.5">
      {score && <ScoreHeader ally={score.ally} enemy={score.enemy} />}
      <Team label={t(lang, "teamAlly")} players={ally} catalog={catalog} lang={lang} accent />
      <Team label={t(lang, "teamEnemy")} players={enemy} catalog={catalog} lang={lang} />
    </div>
  );
}

/** Live round score. Your team is accent-tinted to match the "Your team" section
 * label below, so which number is yours reads at a glance. */
function ScoreHeader({ ally, enemy }: { ally: number; enemy: number }) {
  return (
    <div className="mb-3 flex items-center justify-center gap-3 rounded-xl border border-hairline bg-surface py-2">
      <span className="text-3xl font-bold leading-none tabular-nums text-accent">{ally}</span>
      <span className="text-xl font-semibold leading-none text-fg-mute">:</span>
      <span className="text-3xl font-bold leading-none tabular-nums text-fg">{enemy}</span>
    </div>
  );
}

function Team({
  label,
  players,
  catalog,
  lang,
  accent,
}: {
  label: string;
  players: MatchSeat[];
  catalog: Catalog | null;
  lang: Lang;
  accent?: boolean;
}) {
  if (players.length === 0) return null;
  return (
    <section className="mb-3.5 last:mb-0">
      <div className="mb-1.5 flex items-center gap-2.5 px-1">
        <span className={cn("label text-[11px]", accent ? "text-accent" : "text-fg-mute")}>{label}</span>
        <span className="h-px flex-1 bg-hairline" />
        <span className="text-[11px] tabular-nums text-fg-mute">{players.length}</span>
      </div>
      <div className="flex flex-col gap-1.5">
        {players.map((p, i) => (
          <Row key={`${p.name}-${i}`} seat={p} catalog={catalog} lang={lang} />
        ))}
      </div>
    </section>
  );
}

function Row({ seat, catalog, lang }: { seat: MatchSeat; catalog: Catalog | null; lang: Lang }) {
  const [open, setOpen] = useState(false);
  const s = seat.stats ?? null;
  const agent = seat.agent_uuid ? agentByUuid(catalog, seat.agent_uuid) : undefined;
  const rank = rankByTier(catalog, s?.tier ?? 0);
  const peak = rankByTier(catalog, s?.peak_tier ?? 0);

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-xl border bg-surface",
        seat.self ? "border-accent/45" : "border-hairline",
      )}
    >
      {/* Party indicator: a left strip colored per inferred premade. Always
          rendered (transparent when solo) so grouped/ungrouped rows align. */}
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-[3px]"
        style={{ backgroundColor: partyColor(seat.party_group) }}
      />
      <button
        onClick={() => s && setOpen((v) => !v)}
        className="flex w-full items-center gap-2.5 px-2.5 py-2 text-left"
        aria-expanded={open}
      >
        <AgentAvatar agent={agent} />

        <div className="min-w-0 flex-1">
          <PlayerName name={seat.name} self={seat.self} you={t(lang, "you")} />
          {agent && <div className="truncate text-[10px] text-fg-mute">{agent.displayName}</div>}
        </div>

        {/* History (K/D + recent W/L) on the left… */}
        {s ? (
          <div className="flex shrink-0 flex-col items-end gap-1">
            <span className={cn("text-sm font-bold tabular-nums leading-none", kdColor(s.kd))}>
              {s.kd.toFixed(2)}
            </span>
            <Streak recent={s.recent} />
          </div>
        ) : (
          <span className="shrink-0 text-[10px] text-fg-mute">{t(lang, "loadingStats")}</span>
        )}

        {/* …rank emblems (current + peak) on the right. */}
        <RankCluster rank={rank} peak={peak} lang={lang} />

        {s && (
          <ChevronDown
            className={cn(
              "h-4 w-4 shrink-0 text-fg-mute transition-transform duration-150 ease-[var(--ease-out-quart)]",
              open && "rotate-180",
            )}
          />
        )}
      </button>

      {open && s && (
        <div className="grid grid-cols-4 gap-1.5 border-t border-hairline px-2.5 py-2">
          <Stat label={t(lang, "statKd")} value={s.kd.toFixed(2)} />
          <Stat label={t(lang, "statAdr")} value={Math.round(s.adr).toString()} />
          <Stat label={t(lang, "statHs")} value={`${Math.round(s.hs_pct)}%`} />
          <Stat label={t(lang, "statWin")} value={`${Math.round(s.win_pct)}%`} />
          <RecentRr recent={s.recent} lang={lang} />
          <div className="col-span-4 mt-0.5 px-0.5 text-right text-[10px] tabular-nums text-fg-mute">
            {s.matches} {t(lang, "matchesShort")}
          </div>
        </div>
      )}
    </div>
  );
}

/** Current rank (prominent) with the peak rank below it, both as emblems. */
function RankCluster({
  rank,
  peak,
  lang,
}: {
  rank?: CompetitiveTier;
  peak?: CompetitiveTier;
  lang: Lang;
}) {
  return (
    <div className="flex w-9 shrink-0 flex-col items-center gap-0.5">
      <RankEmblem rank={rank} className="h-8 w-8" />
      {peak && (
        <div className="flex items-center gap-0.5 leading-none">
          <span className="text-[8px] uppercase tracking-wide text-fg-mute">{t(lang, "peak")}</span>
          <RankEmblem rank={peak} className="h-3.5 w-3.5" />
        </div>
      )}
    </div>
  );
}

function RankEmblem({ rank, className }: { rank?: CompetitiveTier; className: string }) {
  if (!rank?.smallIcon) {
    return (
      <div className={cn("grid place-items-center", className)}>
        <span className="h-1 w-1 rounded-full bg-fg-mute" />
      </div>
    );
  }
  return <img src={rank.smallIcon} alt={rank.tierName} title={rank.tierName} className={cn("object-contain", className)} />;
}

function AgentAvatar({ agent }: { agent?: Agent }) {
  return (
    <div className="grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-lg border border-hairline bg-surface-hi">
      {agent?.displayIcon ? (
        <img src={agent.displayIcon} alt={agent.displayName} className="h-full w-full object-cover" />
      ) : (
        <Swords className="h-4 w-4 text-fg-mute" />
      )}
    </div>
  );
}

/** Riot ID with the #tag dimmed, so the name reads first. */
function PlayerName({ name, self, you }: { name: string; self: boolean; you: string }) {
  const hash = name.lastIndexOf("#");
  const game = hash > 0 ? name.slice(0, hash) : name;
  const tag = hash > 0 ? name.slice(hash + 1) : "";
  return (
    <div className={cn("truncate text-sm font-semibold", self ? "text-accent" : "text-fg")}>
      {game || "—"}
      {tag && <span className="font-normal text-fg-mute">#{tag}</span>}
      {self && <span className="ml-1 font-normal text-accent/70">· {you}</span>}
    </div>
  );
}

function Streak({ recent }: { recent: RecentMatch[] }) {
  // Always 5 slots so every row's streak is the same width and the dots line up
  // into a clean column. Results fill from the left (newest first); unused slots
  // are transparent spacers — no ragged edges, no false "neutral" result.
  const r = recent.slice(0, 5);
  return (
    <div className="flex h-1.5 items-center gap-0.5 leading-none">
      {Array.from({ length: 5 }, (_, i) => (
        <span
          key={i}
          className={cn(
            "block h-1.5 w-1.5 shrink-0 rounded-[2px] align-middle",
            i >= r.length ? "bg-transparent" : r[i].won ? "bg-green-500" : "bg-red-500",
          )}
        />
      ))}
    </div>
  );
}

/** Per-match RR deltas (competitive matches only) as colored +/- pills. Hidden
 * when none of the recent matches were ranked. */
function RecentRr({ recent, lang }: { recent: RecentMatch[]; lang: Lang }) {
  const deltas = recent.map((m) => m.rr).filter((rr): rr is number => rr != null);
  if (deltas.length === 0) return null;
  return (
    <div className="col-span-4 mt-0.5 flex items-center gap-1.5 px-0.5">
      <span className="shrink-0 text-[9px] uppercase tracking-wide text-fg-mute">{t(lang, "recentRr")}</span>
      <div className="flex flex-1 flex-wrap justify-end gap-1">
        {deltas.map((d, i) => (
          <span
            key={i}
            className={cn(
              "rounded px-1 py-0.5 text-[10px] font-semibold leading-none tabular-nums",
              d >= 0 ? "bg-green-500/15 text-green-400" : "bg-red-500/15 text-red-400",
            )}
          >
            {d >= 0 ? `+${d}` : d}
          </span>
        ))}
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col items-center gap-0.5 rounded-lg bg-surface-hi py-1.5">
      <span className="text-[9px] uppercase tracking-wide text-fg-mute">{label}</span>
      <span className="text-sm font-semibold tabular-nums text-fg">{value}</span>
    </div>
  );
}

/** Only a strong K/D earns color; the brand red is reserved for the controller. */
function kdColor(kd: number): string {
  return kd >= 1.3 ? "text-ok" : "text-fg";
}

/** Distinct hues for inferred party groups — deliberately not the win/loss
 * green/red or the brand accent, so a party strip never reads as anything else.
 * Cycles if a match somehow has more groups than colors. */
const PARTY_COLORS = ["#38bdf8", "#fbbf24", "#a78bfa", "#fb7185", "#34d399"];
function partyColor(group: number): string {
  if (!group) return "transparent"; // solo → invisible strip, same width (no layout shift)
  return PARTY_COLORS[(group - 1) % PARTY_COLORS.length];
}
