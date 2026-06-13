import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Build output → web/dist, which the Worker serves as static assets
// (cloud/aqua-agent-picker-worker/wrangler.toml [assets].directory = ../../web/dist).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true, // replaces the Phase 0/1 throwaway test page
  },
});
