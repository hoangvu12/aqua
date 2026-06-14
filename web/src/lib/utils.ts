import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** shadcn-style class combiner. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

/**
 * Build a CSS gradient from valorant-api `backgroundGradientColors` (4× RRGGBBAA
 * hex8). The last stop's alpha is `00` (a fade), which CSS #rrggbbaa honours.
 * Falls back to a transparent gradient when colors are missing.
 */
export function agentGradient(colors: string[] | undefined, angle = "180deg"): string {
  if (!colors || colors.length === 0) return "transparent";
  const stops = colors.map((c) => `#${c}`).join(", ");
  return `linear-gradient(${angle}, ${stops})`;
}

/** Nanoseconds (PhaseTimeRemainingNS) → "M:SS", clamped at 0. */
export function formatCountdown(ns: number): string {
  const totalSec = Math.max(0, Math.floor(ns / 1_000_000_000));
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/** Milliseconds → "M:SS", clamped at 0 (the matchmaking search timer). */
export function formatElapsed(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/** Short, stable request id for correlating select/lock results. */
let reqCounter = 0;
export function nextReqId(): string {
  reqCounter += 1;
  return `r${reqCounter}`;
}
