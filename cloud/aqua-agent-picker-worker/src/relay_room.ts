// RelayRoom — one Durable Object instance per device id.
//
// Phase 1 adds auth + pairing on top of the Phase 0 relay:
//   • PC authenticates with a device_secret using trust-on-first-use (TOFU):
//     the DO stores SHA-256(secret) on first connect; later connects must match.
//   • PC mints single-use, 5-min pair codes (control frame `mint_pair_code`).
//   • A phone redeems a code over HTTP `POST /pair {code}` and receives a
//     long-lived token, which it presents as `phone_auth {token}` over the WS.
//   • Unauthenticated sockets cannot relay; only authed pc<->phone frames pass.
//   • `unpair_all` clears every phone token and kicks connected phones.
//   • Pair attempts are rate-limited (brute-force guard on the 8-char code).
//
// State lives in the DO's embedded SQLite (free-tier backend). Per-socket auth
// state rides on the hibernatable socket via serializeAttachment so it survives
// the object being evicted from memory. The device_secret itself never touches
// SQLite (only its hash) and never reaches a phone.

import { DurableObject } from "cloudflare:workers";

const SNAPSHOT_KEY = "last_pc_frame";
// Unambiguous alphabet (length 32 → byte % 32 is bias-free); no 0/O/1/I.
const CODE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";

type Role = "pc" | "phone";
interface Frame {
  type: string;
  reqId?: string;
  data?: any;
}

async function sha256Hex(s: string): Promise<string> {
  const buf = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
function randomHex(bytes: number): string {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  return [...a].map((b) => b.toString(16).padStart(2, "0")).join("");
}
function randomCode(len = 8): string {
  const a = new Uint8Array(len);
  crypto.getRandomValues(a);
  let s = "";
  for (const b of a) s += CODE_ALPHABET[b % CODE_ALPHABET.length];
  return s;
}
function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export class RelayRoom extends DurableObject<Env> {
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    const sql = this.ctx.storage.sql;
    sql.exec(
      `CREATE TABLE IF NOT EXISTS device(id INTEGER PRIMARY KEY CHECK(id=1), secret_hash TEXT NOT NULL);`,
    );
    sql.exec(
      `CREATE TABLE IF NOT EXISTS phone_tokens(token TEXT PRIMARY KEY, created_at INTEGER NOT NULL);`,
    );
    sql.exec(
      `CREATE TABLE IF NOT EXISTS pair_codes(code TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0);`,
    );
    sql.exec(
      `CREATE TABLE IF NOT EXISTS rate(bucket TEXT PRIMARY KEY, count INTEGER NOT NULL, reset_at INTEGER NOT NULL);`,
    );
  }

  // ---- HTTP: WebSocket upgrade (/agent) and pairing (/pair) ----------------

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/pair") return this.handlePairHttp(request);

    // WebSocket upgrade (role in query).
    const role = url.searchParams.get("role");
    if (role !== "pc" && role !== "phone") {
      return new Response("role must be pc or phone", { status: 400 });
    }
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);
    // Role is fixed for the socket's lifetime → store as a hibernation tag.
    this.ctx.acceptWebSocket(server, [role]);
    return new Response(null, { status: 101, webSocket: client });
  }

  private async handlePairHttp(request: Request): Promise<Response> {
    if (request.method !== "POST") return json({ ok: false, error: "method_not_allowed" }, 405);
    if (!this.rateLimit("pair", 10, 60_000)) return json({ ok: false, error: "rate_limited" }, 429);

    let body: any;
    try {
      body = await request.json();
    } catch {
      return json({ ok: false, error: "bad_json" }, 400);
    }
    const code = body?.code;
    if (typeof code !== "string" || code.length === 0) {
      return json({ ok: false, error: "missing_code" }, 400);
    }

    const now = Date.now();
    const row = this.ctx.storage.sql
      .exec<{ code: string; expires: number; used: number }>(
        "SELECT code, expires, used FROM pair_codes WHERE code = ?",
        code,
      )
      .toArray()[0];
    if (!row) return json({ ok: false, error: "invalid_code" }, 400);
    if (row.used) return json({ ok: false, error: "code_used" }, 410);
    if (row.expires < now) {
      this.ctx.storage.sql.exec("DELETE FROM pair_codes WHERE code = ?", code);
      return json({ ok: false, error: "code_expired" }, 410);
    }

    // Consume the code and mint a long-lived phone token.
    this.ctx.storage.sql.exec("UPDATE pair_codes SET used = 1 WHERE code = ?", code);
    const token = randomHex(32);
    this.ctx.storage.sql.exec(
      "INSERT INTO phone_tokens(token, created_at) VALUES (?, ?)",
      token,
      now,
    );
    return json({ ok: true, token });
  }

  // ---- WebSocket frames ----------------------------------------------------

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (typeof message !== "string") return; // protocol is text JSON
    let f: Frame;
    try {
      f = JSON.parse(message);
    } catch {
      return;
    }
    const role: Role = this.ctx.getTags(ws).includes("pc") ? "pc" : "phone";

    // Auth handshake frames are the one thing an unauthenticated socket may send.
    if (role === "pc" && f.type === "pc_auth") return this.handlePcAuth(ws, f);
    if (role === "phone" && f.type === "phone_auth") return this.handlePhoneAuth(ws, f);

    if (!isAuthed(ws)) {
      this.send(ws, { type: "auth_status", data: { ok: false, message: "not authenticated" } });
      return;
    }

    if (role === "pc") {
      if (f.type === "mint_pair_code") return this.handleMint(ws, f);
      if (f.type === "unpair_all") return this.handleUnpair(ws, f);
      // Data frame from PC → fan out to authed phones. Only `state` frames are
      // cached as the snapshot a freshly-paired phone replays (a stray result/
      // auth_status must not overwrite the last good game state).
      if (f.type === "state") await this.ctx.storage.put(SNAPSHOT_KEY, message);
      this.forward("phone", message);
    } else {
      // Data frame from phone → fan in to authed PC socket(s).
      this.forward("pc", message);
    }
  }

  async webSocketClose(ws: WebSocket, code: number, reason: string): Promise<void> {
    try {
      ws.close(code, reason);
    } catch {
      // already closed
    }
  }

  async webSocketError(): Promise<void> {
    // Hibernation runtime cleans up the socket; nothing to do.
  }

  // ---- Auth handlers -------------------------------------------------------

  private async handlePcAuth(ws: WebSocket, f: Frame): Promise<void> {
    const secret = f.data?.secret;
    if (typeof secret !== "string" || secret.length < 16) {
      this.send(ws, { type: "auth_status", data: { ok: false, message: "bad secret" } });
      ws.close(4001, "bad secret");
      return;
    }
    const hash = await sha256Hex(secret);
    const row = this.ctx.storage.sql
      .exec<{ secret_hash: string }>("SELECT secret_hash FROM device WHERE id = 1")
      .toArray()[0];
    if (!row) {
      // Trust on first use: bind this device to the first secret it presents.
      this.ctx.storage.sql.exec("INSERT INTO device(id, secret_hash) VALUES (1, ?)", hash);
    } else if (row.secret_hash !== hash) {
      this.send(ws, { type: "auth_status", data: { ok: false, message: "secret mismatch" } });
      ws.close(4003, "secret mismatch");
      return;
    }
    markAuthed(ws);
    this.send(ws, { type: "auth_status", data: { ok: true, message: "pc authenticated" } });
  }

  private async handlePhoneAuth(ws: WebSocket, f: Frame): Promise<void> {
    const token = f.data?.token;
    let ok = false;
    if (typeof token === "string" && token.length > 0) {
      const row = this.ctx.storage.sql
        .exec("SELECT token FROM phone_tokens WHERE token = ?", token)
        .toArray()[0];
      ok = !!row;
    }
    if (!ok) {
      this.send(ws, { type: "auth_status", data: { ok: false, message: "invalid token" } });
      ws.close(4003, "invalid token");
      return;
    }
    markAuthed(ws);
    this.send(ws, { type: "auth_status", data: { ok: true, message: "phone authenticated" } });
    // Replay the cached snapshot so a freshly-paired phone renders instantly.
    const snap = await this.ctx.storage.get<string>(SNAPSHOT_KEY);
    if (snap) {
      try {
        ws.send(snap);
      } catch {
        // socket already gone
      }
    }
  }

  // ---- PC control handlers -------------------------------------------------

  private handleMint(ws: WebSocket, f: Frame): void {
    let ttl = 300;
    const t = f.data?.ttl_seconds;
    if (typeof t === "number" && Number.isFinite(t)) {
      ttl = Math.min(600, Math.max(1, Math.floor(t)));
    }
    const now = Date.now();
    // Opportunistically prune expired/used codes.
    this.ctx.storage.sql.exec("DELETE FROM pair_codes WHERE expires < ? OR used = 1", now);
    const code = randomCode(8);
    const expires = now + ttl * 1000;
    this.ctx.storage.sql.exec(
      "INSERT INTO pair_codes(code, expires, used) VALUES (?, ?, 0)",
      code,
      expires,
    );
    this.send(ws, {
      type: "result",
      reqId: f.reqId,
      data: { ok: true, code, expires_at: expires, ttl_seconds: ttl },
    });
  }

  private handleUnpair(ws: WebSocket, f: Frame): void {
    this.ctx.storage.sql.exec("DELETE FROM phone_tokens");
    this.ctx.storage.sql.exec("DELETE FROM pair_codes");
    for (const phone of this.ctx.getWebSockets("phone")) {
      try {
        phone.close(4003, "unpaired");
      } catch {
        // ignore
      }
    }
    this.send(ws, { type: "result", reqId: f.reqId, data: { ok: true, message: "all phones unpaired" } });
  }

  // ---- helpers -------------------------------------------------------------

  private forward(targetRole: Role, message: string): void {
    for (const peer of this.ctx.getWebSockets(targetRole)) {
      if (!isAuthed(peer)) continue;
      try {
        peer.send(message);
      } catch {
        // peer closing; ignore
      }
    }
  }

  private send(ws: WebSocket, f: Frame): void {
    try {
      ws.send(JSON.stringify(f));
    } catch {
      // socket closing; ignore
    }
  }

  // Fixed-window rate limiter backed by SQLite. Returns false when over budget.
  private rateLimit(bucket: string, max: number, windowMs: number): boolean {
    const now = Date.now();
    const row = this.ctx.storage.sql
      .exec<{ count: number; reset_at: number }>(
        "SELECT count, reset_at FROM rate WHERE bucket = ?",
        bucket,
      )
      .toArray()[0];
    if (!row || row.reset_at < now) {
      this.ctx.storage.sql.exec(
        "INSERT OR REPLACE INTO rate(bucket, count, reset_at) VALUES (?, ?, ?)",
        bucket,
        1,
        now + windowMs,
      );
      return true;
    }
    if (row.count >= max) return false;
    this.ctx.storage.sql.exec("UPDATE rate SET count = count + 1 WHERE bucket = ?", bucket);
    return true;
  }
}

// Per-socket auth state, persisted on the hibernatable socket.
function isAuthed(ws: WebSocket): boolean {
  const a = ws.deserializeAttachment() as { authed?: boolean } | null;
  return !!a?.authed;
}
function markAuthed(ws: WebSocket): void {
  ws.serializeAttachment({ authed: true });
}
