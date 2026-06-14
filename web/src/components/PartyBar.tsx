import { ChevronUp, X } from "lucide-react";
import type { Catalog, GameStateMsg, PartyMember } from "@/lib/types";
import { rankByTier } from "@/lib/catalog";
import { queueLabel } from "@/lib/queues";
import { t, type Lang } from "@/lib/i18n";
import { cn, formatElapsed } from "@/lib/utils";
import { useElapsed } from "@/lib/use-elapsed";

/**
 * The party summary that lives in the allies-strip slot during pre-match states
 * (menus/lobby/queue/matchfound). It never competes with the agent grid: it's one
 * quiet row showing who's in the party + the queue/accessibility, and it's the
 * handle that opens the full party drawer. While searching it surfaces a live
 * "Searching…" with an inline cancel for the owner, so matchmaking is legible at a
 * glance without opening anything.
 */
export function PartyBar({
  game,
  catalog,
  lang,
  onOpen,
  onCancelSearch,
}: {
  game: GameStateMsg;
  catalog: Catalog | null;
  lang: Lang;
  onOpen: () => void;
  onCancelSearch: () => void;
}) {
  const members = game.party_members;
  const searching = game.state === "queue";
  const found = game.state === "matchfound";
  const elapsed = useElapsed(game.queue_entry_time, searching);
  const shown = members.slice(0, 4);
  const overflow = members.length - shown.length;

  return (
    <div className="flex items-center gap-2 border-t border-hairline px-4 py-2.5">
      <button onClick={onOpen} className="group flex min-w-0 flex-1 items-center gap-3 text-left">
        {members.length > 0 && (
          <span className="flex shrink-0 items-center">
            {shown.map((m, i) => (
              <MiniAvatar key={m.puuid || i} m={m} catalog={catalog} className={i > 0 ? "-ml-2" : ""} />
            ))}
            {overflow > 0 && (
              <span className="-ml-2 grid h-7 w-7 place-items-center rounded-full border border-hairline bg-surface text-[10px] font-semibold text-fg-mute ring-2 ring-bg">
                +{overflow}
              </span>
            )}
          </span>
        )}

        <span className="min-w-0 flex-1">
          {searching ? (
            <span className="flex items-center gap-2 text-sm font-semibold text-fg">
              <span className="relative flex h-2 w-2">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-accent opacity-60" />
                <span className="relative inline-flex h-2 w-2 rounded-full bg-accent" />
              </span>
              {t(lang, "searching")}
              <span className="font-semibold tabular-nums text-fg-dim">{formatElapsed(elapsed)}</span>
            </span>
          ) : found ? (
            <span className="text-sm font-semibold text-ok">{t(lang, "matchFoundShort")}</span>
          ) : (
            <span className="flex min-w-0 items-center gap-2 text-xs text-fg-mute">
              <AccessChip open={game.party_accessibility !== "CLOSED"} lang={lang} />
              {game.queue_id && <span className="truncate text-fg-dim">{queueLabel(lang, game.queue_id)}</span>}
            </span>
          )}
        </span>

        {!searching && (
          <ChevronUp className="ml-auto h-4 w-4 shrink-0 text-fg-mute transition-colors md:group-hover:text-fg-dim" />
        )}
      </button>

      {searching && game.is_party_owner && (
        <button
          onClick={onCancelSearch}
          aria-label={t(lang, "cancelSearch")}
          className="grid h-9 w-9 shrink-0 place-items-center rounded-full border border-hairline bg-surface text-fg-dim md:hover:bg-surface-hi md:hover:text-fg active:bg-surface-hi"
        >
          <X className="h-4 w-4" />
        </button>
      )}
      {searching && !game.is_party_owner && (
        <button onClick={onOpen} aria-label={t(lang, "party")}>
          <ChevronUp className="h-4 w-4 shrink-0 text-fg-mute" />
        </button>
      )}
    </div>
  );
}

/** Small round member avatar: rank emblem when stats are loaded, else an initial.
 * The ring-2 ring-bg separates overlapping avatars (and is never clipped — the
 * badge-clipping pattern fixed in AlliesStrip). */
export function MiniAvatar({
  m,
  catalog,
  className,
}: {
  m: PartyMember;
  catalog: Catalog | null;
  className?: string;
}) {
  const rank = m.stats && m.stats.tier > 0 ? rankByTier(catalog, m.stats.tier) : undefined;
  const initial = (m.self ? "Y" : m.name || "?").trim().charAt(0).toUpperCase();
  return (
    <span
      className={cn(
        "grid h-7 w-7 shrink-0 place-items-center overflow-hidden rounded-full border bg-surface ring-2 ring-bg",
        m.self ? "border-accent/60" : "border-hairline",
        className,
      )}
    >
      {rank?.smallIcon ? (
        <img src={rank.smallIcon} alt="" className="h-5 w-5 object-contain" />
      ) : (
        <span className="text-[10px] font-semibold text-fg-mute">{initial}</span>
      )}
    </span>
  );
}

function AccessChip({ open, lang }: { open: boolean; lang: Lang }) {
  return (
    <span
      className={cn(
        "label rounded-full px-1.5 py-0.5 text-[10px]",
        open ? "bg-ok/15 text-ok" : "bg-surface-hi text-fg-mute",
      )}
    >
      {open ? t(lang, "partyOpen") : t(lang, "partyClosed")}
    </span>
  );
}
