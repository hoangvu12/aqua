// Pairing + credential storage. The PC shows a QR/URL of the form
//   https://aqua.nguyenvu.dev/?code=XXXX&device=<deviceId>
// The phone redeems the code at POST /pair {code, device} → a long-lived token,
// stored in localStorage and presented later as `phone_auth {token}`.

const DEVICE_KEY = "aqua_device";
const TOKEN_KEY = "aqua_token";

/** Base origin for relay endpoints. Empty = same-origin (production, served by
 *  the Worker). In dev, VITE_RELAY_ORIGIN targets the local mock for hot reload. */
export const RELAY_ORIGIN = import.meta.env.VITE_RELAY_ORIGIN ?? "";
/** ws:// base derived from RELAY_ORIGIN; empty when same-origin. */
export const RELAY_WS_BASE = RELAY_ORIGIN ? RELAY_ORIGIN.replace(/^http/, "ws") : "";

export interface Creds {
  device: string;
  token: string;
}

export function loadCreds(): Creds | null {
  const device = localStorage.getItem(DEVICE_KEY);
  const token = localStorage.getItem(TOKEN_KEY);
  if (device && token) return { device, token };
  return null;
}

export function saveCreds(creds: Creds): void {
  localStorage.setItem(DEVICE_KEY, creds.device);
  localStorage.setItem(TOKEN_KEY, creds.token);
}

export function clearCreds(): void {
  localStorage.removeItem(DEVICE_KEY);
  localStorage.removeItem(TOKEN_KEY);
}

/** Read ?code=&device= from the URL (the PC's pairing link). */
export function readPairLink(): { code: string; device: string } | null {
  const p = new URLSearchParams(location.search);
  const code = p.get("code");
  const device = p.get("device");
  if (code && device) return { code, device };
  return null;
}

/** Strip ?code (and friends) from the address bar after a successful pair. */
export function clearPairLinkFromUrl(): void {
  history.replaceState({}, "", location.pathname);
}

export interface PairResult {
  ok: boolean;
  token?: string;
  error?: string;
}

/** Redeem a pair code. Same-origin, so no CORS handling needed. */
export async function redeemCode(code: string, device: string): Promise<PairResult> {
  try {
    const res = await fetch(`${RELAY_ORIGIN}/pair`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code, device }),
    });
    const data = (await res.json()) as PairResult;
    return data;
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : "network_error" };
  }
}
