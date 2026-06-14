import { useEffect, useState } from "react";

/**
 * Live elapsed milliseconds since `sinceMs` (a unix-millis timestamp), ticking
 * once a second while `active`. Used for the matchmaking search timer: because it
 * derives from the party's real queue-entry time (pushed by the PC), it stays
 * accurate across drawer open/close and phone reconnects, not just from when the
 * phone happened to notice. Returns 0 when inactive or unset; clamps negatives
 * (minor PC↔phone clock skew) to 0.
 */
export function useElapsed(sinceMs: number, active: boolean): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!active || !sinceMs) return;
    setNow(Date.now()); // resync immediately on (re)activation
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [active, sinceMs]);

  if (!active || !sinceMs) return 0;
  return Math.max(0, now - sinceMs);
}
