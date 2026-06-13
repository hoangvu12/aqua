import { useState } from "react";
import { Link2, Loader2 } from "lucide-react";
import { clearPairLinkFromUrl, redeemCode, saveCreds, type Creds } from "@/lib/pair";
import { t, type Lang } from "@/lib/i18n";
import { Button } from "./ui/button";

/**
 * Full-screen first-run pairing view (not a modal — plan §UI). If the URL carries
 * the PC's ?code=&device= link, offer to redeem it; otherwise instruct the user
 * to open the pairing link shown on their PC.
 */
export function Pairing({
  pairLink,
  onPaired,
  lang,
  onToggleLang,
}: {
  pairLink: { code: string; device: string } | null;
  onPaired: (creds: Creds) => void;
  lang: Lang;
  onToggleLang: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pair = async () => {
    if (!pairLink) return;
    setBusy(true);
    setError(null);
    const res = await redeemCode(pairLink.code, pairLink.device);
    if (res.ok && res.token) {
      const creds = { device: pairLink.device, token: res.token };
      saveCreds(creds);
      clearPairLinkFromUrl();
      onPaired(creds);
    } else {
      setError(res.error ?? "pair_failed");
      setBusy(false);
    }
  };

  return (
    <main className="flex min-h-dvh flex-col px-6 py-8">
      <div className="flex justify-end">
        <button
          onClick={onToggleLang}
          className="rounded-full border border-hairline px-2.5 py-1 text-xs font-semibold text-fg-dim active:bg-surface"
        >
          {lang === "vi" ? "VI" : "EN"}
        </button>
      </div>

      <div className="flex flex-1 flex-col items-center justify-center text-center">
        <img src="/android-chrome-192x192.png" alt="" className="mb-6 h-20 w-20 rounded-[var(--radius-card)]" />
        <h1 className="text-2xl font-bold">{t(lang, "appName")}</h1>

        {pairLink ? (
          <>
            <p className="mt-2 max-w-xs text-sm text-fg-dim">{t(lang, "pairTitle")}</p>
            <div className="mt-5 rounded-[var(--radius-card)] border border-hairline bg-surface px-6 py-3">
              <span className="font-mono text-2xl font-bold tracking-[0.2em] text-fg">
                {pairLink.code}
              </span>
            </div>
            <Button
              variant="accent"
              size="lg"
              className="mt-6 w-full max-w-xs"
              disabled={busy}
              onClick={pair}
            >
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}
              {busy ? t(lang, "pairing") : t(lang, "pairButton")}
            </Button>
            {error && <p className="mt-3 text-sm font-semibold text-accent">{t(lang, "pairFailed")}</p>}
          </>
        ) : (
          <>
            <p className="mt-3 max-w-xs text-sm text-fg-dim">{t(lang, "pairBody")}</p>
            <p className="mt-6 max-w-xs text-sm text-fg-mute">{t(lang, "needLink")}</p>
          </>
        )}
      </div>
    </main>
  );
}
