import type { ConnStatus } from "@/lib/relay";
import { t, type Lang } from "@/lib/i18n";
import { cn } from "@/lib/utils";

/** Connection status pill: dot + label. Reflects relay transport health only. */
export function ConnectionChip({ conn, lang }: { conn: ConnStatus; lang: Lang }) {
  const map = {
    connecting: { dot: "bg-warn", label: t(lang, "loading"), pulse: true },
    connected: { dot: "bg-ok", label: t(lang, "connected"), pulse: false },
    reconnecting: { dot: "bg-warn", label: t(lang, "reconnecting"), pulse: true },
    unauthorized: { dot: "bg-accent", label: t(lang, "disconnected"), pulse: false },
  } as const;
  const s = map[conn];
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-surface/70 px-2.5 py-1 text-xs font-semibold text-fg-dim">
      <span className={cn("h-1.5 w-1.5 rounded-full", s.dot, s.pulse && "animate-pulse")} />
      {s.label}
    </span>
  );
}
