/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Dev-only override pointing the relay endpoints at the local mock
   *  (`bun run mock`). Unset in production → endpoints stay same-origin. */
  readonly VITE_RELAY_ORIGIN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
