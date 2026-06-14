import { useState } from "react";
import { Check, Copy, Crown, Loader2, LogOut, Search, UserMinus, X } from "lucide-react";
import type { Catalog, GameStateMsg, PartyMember, ResultData } from "@/lib/types";
import type { PartyActions } from "@/lib/relay";
import { rankByTier } from "@/lib/catalog";
import { QUEUES, queueLabel } from "@/lib/queues";
import { t, type Lang } from "@/lib/i18n";
import { cn, formatElapsed } from "@/lib/utils";
import { useElapsed } from "@/lib/use-elapsed";
import { Drawer } from "./ui/drawer";
import { Button } from "./ui/button";
import { MiniAvatar } from "./PartyBar";

/**
 * The party cockpit. A bottom sheet over the agent grid — never replacing it — so
 * the one-decisive-action surface stays put. Top→bottom: accessibility + invite
 * code, the member roster (with kick), the queue picker, and a sticky Find Match /
 * Cancel CTA with Leave party below it. Owner-only controls are visibly disabled
 * for non-owners (PRODUCT principle 4: honest about what the tool can do), and the
 * PC re-checks ownership before issuing any Riot call.
 */
export function PartyDrawer({
  open,
  onClose,
  game,
  catalog,
  lang,
  party,
}: {
  open: boolean;
  onClose: () => void;
  game: GameStateMsg;
  catalog: Catalog | null;
  lang: Lang;
  party: PartyActions;
}) {
  const owner = game.is_party_owner;
  const accessOpen = game.party_accessibility !== "CLOSED";
  const code = game.party_invite_code;
  const searching = game.state === "queue";
  const elapsed = useElapsed(game.queue_entry_time, searching);

  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Which slow action is in flight, so its button can show a spinner. Most party
  // actions reflect instantly via optimistic state; only the ones whose result we
  // can't predict (generate/join code) need an explicit "working" cue.
  const [pending, setPending] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [joinCode, setJoinCode] = useState("");

  // Run a party action: clear notice, await, surface any failure message inline.
  const run = async (p: Promise<ResultData>, id?: string): Promise<ResultData> => {
    setBusy(true);
    setPending(id ?? null);
    setNotice(null);
    const r = await p;
    setBusy(false);
    setPending(null);
    if (!r.ok) setNotice(r.message);
    return r;
  };

  const copyCode = async () => {
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setNotice(code); // clipboard blocked → at least show it to copy by hand
    }
  };

  const submitJoin = async () => {
    const c = joinCode.trim();
    if (!c) return;
    const r = await run(party.joinByCode(c), "join");
    if (r.ok) {
      setJoinCode("");
      onClose();
    }
  };

  const leave = async () => {
    const r = await run(party.leave());
    if (r.ok) onClose();
  };

  return (
    <Drawer open={open} onClose={onClose} labelledBy="party-drawer-title">
      <div className="no-scrollbar flex-1 overflow-y-auto px-4 pb-3 pt-1">
        {/* Header: title + accessibility segmented toggle (owner-only). */}
        <div className="flex items-center justify-between gap-3 py-2">
          <h2 id="party-drawer-title" className="flex items-baseline gap-2">
            <span className="text-lg font-bold">{t(lang, "party")}</span>
            <span className="text-sm font-medium tabular-nums text-fg-mute">
              {game.party_members.length}
              {game.party_max_size > 0 && `/${game.party_max_size}`}
            </span>
          </h2>
          <Segmented
            value={accessOpen ? "open" : "closed"}
            disabled={!owner || busy}
            options={[
              { value: "open", label: t(lang, "partyOpen") },
              { value: "closed", label: t(lang, "partyClosed") },
            ]}
            onChange={(v) => run(party.setAccessibility(v === "open"))}
          />
        </div>

        {/* Invite code. */}
        <Section label={t(lang, "inviteCode")}>
          {code ? (
            <div className="flex items-center gap-2">
              <span className="flex-1 truncate rounded-[var(--radius-tile)] border border-hairline bg-surface px-3 py-2.5 font-mono text-lg font-bold tracking-[0.2em]">
                {code}
              </span>
              <Button variant="surface" size="md" onClick={copyCode} className="shrink-0">
                {copied ? <Check className="h-4 w-4 text-ok" /> : <Copy className="h-4 w-4" />}
                {copied ? t(lang, "copied") : t(lang, "copy")}
              </Button>
              {owner && (
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => run(party.disableCode())}
                  disabled={busy}
                  aria-label={t(lang, "disableCode")}
                  className="shrink-0"
                >
                  <X className="h-4 w-4" />
                </Button>
              )}
            </div>
          ) : (
            <Button
              variant="surface"
              size="md"
              onClick={() => run(party.generateCode(), "generate")}
              disabled={!owner || busy}
              className="w-full"
            >
              {pending === "generate" && <Loader2 className="h-4 w-4 animate-spin" />}
              {t(lang, "generateCode")}
            </Button>
          )}

          {/* Join by code is open to everyone (you're joining someone else). */}
          <form
            className="mt-2 flex items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              submitJoin();
            }}
          >
            <input
              value={joinCode}
              onChange={(e) => setJoinCode(e.target.value.toUpperCase())}
              placeholder={t(lang, "joinCodePlaceholder")}
              inputMode="text"
              autoCapitalize="characters"
              autoCorrect="off"
              spellCheck={false}
              maxLength={16}
              className="h-12 flex-1 rounded-[var(--radius-tile)] border border-hairline bg-surface px-3 font-mono text-base tracking-[0.15em] text-fg outline-none placeholder:tracking-normal placeholder:text-fg-mute focus-visible:ring-2 focus-visible:ring-accent"
            />
            <Button type="submit" variant="surface" size="md" disabled={!joinCode.trim() || busy}>
              {pending === "join" && <Loader2 className="h-4 w-4 animate-spin" />}
              {t(lang, "join")}
            </Button>
          </form>
        </Section>

        {/* Member roster. */}
        <Section label={t(lang, "partyMembers")}>
          <ul className="flex flex-col gap-1">
            {game.party_members.map((m, i) => (
              <MemberRow
                key={m.puuid || i}
                m={m}
                catalog={catalog}
                lang={lang}
                canKick={owner && !m.self}
                busy={busy}
                onKick={() => run(party.kick(m.puuid))}
              />
            ))}
          </ul>
        </Section>

        {/* Queue picker (owner-only). */}
        <Section label={t(lang, "queue")}>
          <div className={cn("flex flex-wrap gap-2", !owner && "pointer-events-none opacity-45")}>
            {QUEUES.map((q) => {
              const active = game.queue_id === q.id;
              return (
                <button
                  key={q.id}
                  onClick={() => run(party.setQueue(q.id))}
                  disabled={busy}
                  aria-pressed={active}
                  className={cn(
                    "rounded-full border px-3.5 py-1.5 text-sm font-semibold transition-colors duration-150 ease-[var(--ease-out-quart)]",
                    active
                      ? "border-accent bg-accent text-on-accent"
                      : "border-hairline bg-surface text-fg-dim md:hover:border-fg-mute/70 md:hover:text-fg active:bg-surface-hi",
                  )}
                >
                  {queueLabel(lang, q.id)}
                </button>
              );
            })}
          </div>
        </Section>

        {notice && (
          <p className="mt-3 rounded-[var(--radius-tile)] bg-accent/12 px-3 py-2 text-sm text-accent">
            {notice}
          </p>
        )}
      </div>

      {/* Sticky CTA zone. */}
      <div className="shrink-0 space-y-2 border-t border-hairline px-4 pt-3">
        {searching ? (
          <Button
            variant="surface"
            size="lg"
            onClick={() => run(party.stopMatchmaking())}
            disabled={!game.is_party_owner || busy}
            className="w-full"
          >
            <X className="h-5 w-5" />
            {t(lang, "cancelSearch")}
            <span className="tabular-nums text-fg-mute">{formatElapsed(elapsed)}</span>
          </Button>
        ) : (
          <Button
            variant="accent"
            size="lg"
            onClick={() => run(party.startMatchmaking())}
            disabled={!owner || !game.queue_id || busy}
            className="w-full"
          >
            <Search className="h-5 w-5" />
            {t(lang, "findMatch")}
          </Button>
        )}

        <button
          onClick={leave}
          disabled={busy}
          className="flex w-full items-center justify-center gap-2 py-1.5 text-sm font-medium text-fg-mute md:hover:text-fg-dim active:text-fg-dim disabled:opacity-45"
        >
          <LogOut className="h-4 w-4" />
          {t(lang, "leaveParty")}
        </button>

        {!owner && (
          <p className="pb-1 text-center text-[11px] leading-tight text-fg-mute">
            {t(lang, "ownerOnlyHint")}
          </p>
        )}
      </div>
    </Drawer>
  );
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="mt-4">
      <p className="label mb-2 text-[11px] text-fg-mute">{label}</p>
      {children}
    </section>
  );
}

function MemberRow({
  m,
  catalog,
  lang,
  canKick,
  busy,
  onKick,
}: {
  m: PartyMember;
  catalog: Catalog | null;
  lang: Lang;
  canKick: boolean;
  busy: boolean;
  onKick: () => void;
}) {
  const rank = m.stats && m.stats.tier > 0 ? rankByTier(catalog, m.stats.tier) : undefined;
  return (
    <li className="flex items-center gap-3 rounded-[var(--radius-tile)] bg-surface px-2.5 py-2">
      <MiniAvatar m={m} catalog={catalog} />
      <div className="min-w-0 flex-1">
        <p className={cn("truncate text-sm font-semibold", m.self ? "text-accent" : "text-fg")}>
          {m.self ? t(lang, "you") : m.name || "—"}
        </p>
        {rank && <p className="truncate text-[11px] text-fg-mute">{rank.tierName}</p>}
      </div>
      {m.is_owner && (
        <span className="inline-flex items-center gap-1 text-[11px] font-medium text-fg-mute">
          <Crown className="h-3.5 w-3.5" />
          {t(lang, "owner")}
        </span>
      )}
      {canKick && (
        <button
          onClick={onKick}
          disabled={busy}
          aria-label={t(lang, "kick")}
          className="grid h-9 w-9 shrink-0 place-items-center rounded-full text-fg-mute md:hover:bg-surface-hi md:hover:text-accent active:bg-surface-hi active:text-accent disabled:opacity-45"
        >
          <UserMinus className="h-4 w-4" />
        </button>
      )}
    </li>
  );
}

/** A two-option segmented control (Open/Closed). Disabled state dims + blocks. */
function Segmented({
  value,
  options,
  disabled,
  onChange,
}: {
  value: string;
  options: { value: string; label: string }[];
  disabled?: boolean;
  onChange: (v: string) => void;
}) {
  return (
    <div
      className={cn(
        "inline-flex rounded-full border border-hairline bg-surface p-0.5",
        disabled && "pointer-events-none opacity-45",
      )}
    >
      {options.map((o) => {
        const active = o.value === value;
        return (
          <button
            key={o.value}
            onClick={() => !active && onChange(o.value)}
            aria-pressed={active}
            className={cn(
              "rounded-full px-3 py-1 text-xs font-semibold transition-colors duration-150 ease-[var(--ease-out-quart)]",
              active ? "bg-accent text-on-accent" : "text-fg-mute md:hover:text-fg-dim",
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
