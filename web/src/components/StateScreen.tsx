import { Loader2, PowerOff, TriangleAlert, Gamepad2 } from "lucide-react";
import { t, type Lang } from "@/lib/i18n";

type Kind = "loading" | "offline" | "error" | "ingame";

/** Full-screen takeover for states where the agent grid isn't relevant. */
export function StateScreen({ kind, lang }: { kind: Kind; lang: Lang }) {
  const config = {
    loading: {
      icon: <Loader2 className="h-9 w-9 animate-spin text-fg-mute" />,
      title: t(lang, "loading"),
      body: t(lang, "waitingGame"),
    },
    offline: {
      icon: <PowerOff className="h-9 w-9 text-fg-mute" />,
      title: t(lang, "pcOfflineTitle"),
      body: t(lang, "pcOfflineBody"),
    },
    error: {
      icon: <TriangleAlert className="h-9 w-9 text-warn" />,
      title: t(lang, "errorTitle"),
      body: t(lang, "errorBody"),
    },
    ingame: {
      icon: <Gamepad2 className="h-9 w-9 text-ok" />,
      title: t(lang, "state_ingame"),
      body: "",
    },
  }[kind];

  return (
    <div className="flex flex-1 flex-col items-center justify-center px-8 text-center">
      <div className="mb-5 grid h-20 w-20 place-items-center rounded-full border border-hairline bg-surface">
        {config.icon}
      </div>
      <h2 className="text-xl font-bold">{config.title}</h2>
      {config.body && <p className="mt-2 max-w-xs text-sm text-fg-dim">{config.body}</p>}
    </div>
  );
}
