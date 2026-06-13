// Phase 1 auth + pairing test (run with: bun test-pairing.ts).
// Drives the RelayRoom DO directly via raw WebSockets + the /pair HTTP route —
// no dependency on the Go binary. Each run uses a fresh random device id so the
// DO's persisted SQLite (TOFU hash, rate buckets) never leaks across runs.
const WS = process.env.WS_BASE ?? "ws://127.0.0.1:8787";
const HTTP = process.env.HTTP_BASE ?? "http://127.0.0.1:8787";

const rnd = () => Math.random().toString(16).slice(2);
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

let failed = false;
function assert(cond: boolean, label: string, detail?: unknown) {
  console.log(`${cond ? "✅" : "❌"} ${label}${detail !== undefined ? `  ${JSON.stringify(detail)}` : ""}`);
  if (!cond) failed = true;
}

class Sock {
  ws: WebSocket;
  queue: any[] = [];
  waiters: ((f: any) => void)[] = [];
  closed: Promise<CloseEvent>;
  constructor(ws: WebSocket) {
    this.ws = ws;
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(String(e.data));
      const w = this.waiters.shift();
      if (w) w(f);
      else this.queue.push(f);
    });
    this.closed = new Promise((res) => ws.addEventListener("close", (e) => res(e as CloseEvent), { once: true }));
  }
  send(obj: unknown) { this.ws.send(JSON.stringify(obj)); }
  next(timeoutMs = 4000): Promise<any> {
    if (this.queue.length) return Promise.resolve(this.queue.shift());
    return new Promise((res, rej) => {
      const t = setTimeout(() => rej(new Error("timeout waiting for frame")), timeoutMs);
      this.waiters.push((f) => { clearTimeout(t); res(f); });
    });
  }
  close() { this.ws.close(); }
}

async function open(role: string, device: string): Promise<Sock> {
  const ws = new WebSocket(`${WS}/agent?role=${role}&device=${device}`);
  await new Promise<void>((res, rej) => {
    ws.addEventListener("open", () => res(), { once: true });
    ws.addEventListener("error", (e) => rej(e), { once: true });
  });
  return new Sock(ws);
}
async function pair(device: string, code: string): Promise<{ status: number; body: any }> {
  const res = await fetch(`${HTTP}/pair`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ code, device }),
  });
  return { status: res.status, body: await res.json() };
}

const device = `test-${rnd()}`;
const SECRET = "a".repeat(64); // 32 bytes hex, like the real device secret

// 1) PC auth establishes TOFU.
const pc = await open("pc", device);
pc.send({ type: "pc_auth", data: { secret: SECRET } });
let f = await pc.next();
assert(f.type === "auth_status" && f.data.ok === true, "pc auth (TOFU first use) ok", f.data);

// 2) Secret mismatch is rejected and the socket is closed.
const pcBad = await open("pc", device);
pcBad.send({ type: "pc_auth", data: { secret: "b".repeat(64) } });
f = await pcBad.next();
assert(f.type === "auth_status" && f.data.ok === false, "wrong secret rejected", f.data);
const closedBad = await Promise.race([pcBad.closed.then((e) => e.code), sleep(2000).then(() => -1)]);
assert(closedBad === 4003, "wrong-secret socket closed (4003)", closedBad);

// 3) Mint a pair code.
pc.send({ type: "mint_pair_code", reqId: "m1", data: { ttl_seconds: 300 } });
f = await pc.next();
assert(f.type === "result" && f.reqId === "m1" && typeof f.data.code === "string" && f.data.code.length === 8,
  "pair code minted", f.data);
const code = f.data.code as string;

// 4) Wrong code rejected.
let r = await pair(device, "ZZZZ0000");
assert(r.status === 400 && r.body.error === "invalid_code", "wrong code rejected", r.body);

// 5) Happy-path pair → token.
r = await pair(device, code);
assert(r.status === 200 && r.body.ok === true && typeof r.body.token === "string", "pair happy path → token", r.body);
const token = r.body.token as string;

// 6) Reusing a consumed code is rejected.
r = await pair(device, code);
assert(r.status === 410 && r.body.error === "code_used", "used code rejected", r.body);

// 7) Phone auth with the token succeeds.
const phone = await open("phone", device);
phone.send({ type: "phone_auth", data: { token } });
f = await phone.next();
assert(f.type === "auth_status" && f.data.ok === true, "phone auth with token ok", f.data);

// 8) Phone auth with a bogus token is rejected and closed.
const phoneBad = await open("phone", device);
phoneBad.send({ type: "phone_auth", data: { token: "not-a-real-token" } });
f = await phoneBad.next();
assert(f.type === "auth_status" && f.data.ok === false, "bad token rejected", f.data);
const phoneBadClosed = await Promise.race([phoneBad.closed.then((e) => e.code), sleep(2000).then(() => -1)]);
assert(phoneBadClosed === 4003, "bad-token socket closed (4003)", phoneBadClosed);

// 9) Authenticated relay round-trip: phone ping → pc → pong → phone.
phone.send({ type: "ping", data: { n: 7, text: "hi" } });
const pingAtPc = await pc.next();
assert(pingAtPc.type === "ping" && pingAtPc.data.n === 7, "pc received forwarded ping", pingAtPc.data);
pc.send({ type: "pong", data: { n: 7, text: "pong from pc" } });
const pongAtPhone = await phone.next();
assert(pongAtPhone.type === "pong" && pongAtPhone.data.n === 7, "phone received pong", pongAtPhone.data);

// 10) Unauthenticated phone cannot relay.
const phoneNoAuth = await open("phone", device);
phoneNoAuth.send({ type: "ping", data: { n: 99 } });
f = await phoneNoAuth.next();
assert(f.type === "auth_status" && f.data.ok === false, "unauthenticated relay blocked", f.data);
phoneNoAuth.close();

// 11) Expired code: mint with a 1s TTL, wait it out, then redeem.
pc.send({ type: "mint_pair_code", reqId: "m2", data: { ttl_seconds: 1 } });
f = await pc.next();
const shortCode = f.data.code as string;
await sleep(1300);
r = await pair(device, shortCode);
assert(r.status === 410 && r.body.error === "code_expired", "expired code rejected", r.body);

// 12) Unpair-all clears tokens and kicks connected phones.
pc.send({ type: "unpair_all", reqId: "u1" });
f = await pc.next();
assert(f.type === "result" && f.data.ok === true, "unpair_all result ok", f.data);
const phoneKicked = await Promise.race([phone.closed.then((e) => e.code), sleep(2000).then(() => -1)]);
assert(phoneKicked === 4003, "connected phone kicked on unpair (4003)", phoneKicked);
const reauth = await open("phone", device);
reauth.send({ type: "phone_auth", data: { token } });
f = await reauth.next();
assert(f.type === "auth_status" && f.data.ok === false, "old token invalid after unpair", f.data);
reauth.close();

// 13) Rate limiting on /pair (isolated fresh device → fresh bucket).
const rlDevice = `rl-${rnd()}`;
let got429 = false;
for (let i = 0; i < 12; i++) {
  const rr = await pair(rlDevice, "NOPENOPE");
  if (rr.status === 429) got429 = true;
}
assert(got429, "pair endpoint rate-limited after burst (429)");

pc.close();
phone.close();
console.log(failed ? "\nFAIL" : "\nPASS");
process.exit(failed ? 1 : 0);
