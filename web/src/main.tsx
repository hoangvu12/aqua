import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App.tsx";
import "./index.css";

// The game state is push-only (the PC streams it over the WS), so nothing here is
// ever fetched or refetched — queries live forever and never go stale on their
// own. See lib/relay.ts for how frames seed the cache and how commands mutate it.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: Infinity, gcTime: Infinity, retry: false, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
);

// PWA: register the service worker in production (the dev server / mock relay has
// no Worker mirror, so skip it there to avoid caching stale dev assets).
if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // best-effort; the app works fine without it
    });
  });
}
