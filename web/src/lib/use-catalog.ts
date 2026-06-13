import { useEffect, useState } from "react";
import { loadCatalog } from "./catalog";
import type { Catalog } from "./types";

export interface CatalogState {
  catalog: Catalog | null;
  loading: boolean;
  error: string | null;
}

/**
 * Loads the valorant-api catalog for the current game locale, refetching when the
 * resolved language changes. Serves from the localStorage cache first (loadCatalog
 * handles version-keyed caching + stale fallback), so this is instant on repeat.
 */
export function useCatalog(gameLocale: string | undefined): CatalogState {
  const [state, setState] = useState<CatalogState>({
    catalog: null,
    loading: true,
    error: null,
  });

  useEffect(() => {
    let cancelled = false;
    setState((s) => ({ ...s, loading: true, error: null }));
    loadCatalog(gameLocale)
      .then((catalog) => {
        if (!cancelled) setState({ catalog, loading: false, error: null });
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setState({
            catalog: null,
            loading: false,
            error: e instanceof Error ? e.message : "catalog error",
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [gameLocale]);

  return state;
}
