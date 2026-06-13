import type { Agent, Catalog, GameMap } from "./types";

// In production the Worker mirrors the catalog at same-origin /api (and rewrites
// embedded art URLs to /cdn) — no third-party hotlink at runtime. In dev there is
// no Worker (vite / `bun run mock`), so hit valorant-api directly.
const API = import.meta.env.PROD ? "/api" : "https://valorant-api.com/v1";

// valorant-api supported language codes. We map the game locale onto one of these
// (default en-US). https://dash.valorant-api.com/ documents the set.
const SUPPORTED = new Set([
  "ar-AE", "de-DE", "en-US", "es-ES", "es-MX", "fr-FR", "id-ID", "it-IT", "ja-JP",
  "ko-KR", "pl-PL", "pt-BR", "ru-RU", "th-TH", "tr-TR", "vi-VN", "zh-CN", "zh-TW",
]);

/** Resolve a game locale (e.g. "vi-VN", "en-US") to a valorant-api language code. */
export function apiLanguage(gameLocale: string | undefined): string {
  if (!gameLocale) return "en-US";
  if (SUPPORTED.has(gameLocale)) return gameLocale;
  // Match on the language prefix (e.g. "vi" → "vi-VN").
  const prefix = gameLocale.split("-")[0]?.toLowerCase();
  for (const code of SUPPORTED) {
    if (code.toLowerCase().startsWith(prefix + "-")) return code;
  }
  return "en-US";
}

const cacheKey = (lang: string) => `aqua_catalog_${lang}`;

async function fetchVersion(): Promise<string> {
  const res = await fetch(`${API}/version`);
  if (!res.ok) throw new Error(`version ${res.status}`);
  const j = await res.json();
  return j?.data?.version ?? "unknown";
}

async function fetchAgents(lang: string): Promise<Agent[]> {
  const res = await fetch(`${API}/agents?isPlayableCharacter=true&language=${lang}`);
  if (!res.ok) throw new Error(`agents ${res.status}`);
  const j = await res.json();
  return (j?.data ?? []) as Agent[];
}

async function fetchMaps(lang: string): Promise<GameMap[]> {
  const res = await fetch(`${API}/maps?language=${lang}`);
  if (!res.ok) throw new Error(`maps ${res.status}`);
  const j = await res.json();
  return (j?.data ?? []) as GameMap[];
}

/**
 * Load the agent + map catalog for a locale, served from localStorage when the
 * cached entry matches the current valorant-api version (plan §Assets). On any
 * network error we fall back to a stale cache if present, so the UI still renders.
 */
export async function loadCatalog(gameLocale: string | undefined): Promise<Catalog> {
  const language = apiLanguage(gameLocale);
  const key = cacheKey(language);
  const cachedRaw = localStorage.getItem(key);
  const cached: Catalog | null = cachedRaw ? safeParse(cachedRaw) : null;

  let version: string;
  try {
    version = await fetchVersion();
  } catch {
    if (cached) return cached;
    throw new Error("catalog unavailable (offline, no cache)");
  }

  if (cached && cached.version === version) return cached;

  try {
    const [agents, maps] = await Promise.all([fetchAgents(language), fetchMaps(language)]);
    const catalog: Catalog = { version, language, agents, maps };
    try {
      localStorage.setItem(key, JSON.stringify(catalog));
    } catch {
      // quota / private mode — fine, just skip caching
    }
    return catalog;
  } catch (e) {
    if (cached) return cached; // serve stale rather than break the UI
    throw e;
  }
}

function safeParse(s: string): Catalog | null {
  try {
    return JSON.parse(s) as Catalog;
  } catch {
    return null;
  }
}

// ── Lookup helpers ───────────────────────────────────────────────────────────

export function agentByUuid(catalog: Catalog | null, uuid: string): Agent | undefined {
  return catalog?.agents.find((a) => a.uuid === uuid);
}

/**
 * Find the map for a GLZ `MapID` path by matching its `mapUrl` (NOT displayName —
 * e.g. Haven's MapID is /Game/Maps/Triad/Triad). Trailing-segment tolerant.
 */
export function mapByMapId(catalog: Catalog | null, mapId: string): GameMap | undefined {
  if (!catalog || !mapId) return undefined;
  return catalog.maps.find((m) => m.mapUrl && m.mapUrl === mapId);
}

/** Distinct roles in catalog order (Duelist, Initiator, Controller, Sentinel). */
export function rolesOf(catalog: Catalog | null): { uuid: string; name: string }[] {
  if (!catalog) return [];
  const seen = new Map<string, string>();
  for (const a of catalog.agents) {
    if (a.role && !seen.has(a.role.uuid)) seen.set(a.role.uuid, a.role.displayName);
  }
  return [...seen].map(([uuid, name]) => ({ uuid, name }));
}
