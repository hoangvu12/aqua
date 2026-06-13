// Phase 2 picker integration test (run with: bun test-picker.ts).
// Requires `wrangler dev` running and Aqua.exe started with -sim, paired, with
// CODE + DEVICE passed via env (the runner script wires this up). It pairs a
// phone, then watches simulated game state flow through the relay and drives a
// lock, asserting the state machine + optimistic→reconcile + select/lock bridge.
const WS = process.env.WS_BASE ?? "ws://127.0.0.1:8787";
const HTTP = process.env.HTTP_BASE ?? "http://127.0.0.1:8787";
const CODE = process.env.CODE!;
const DEVICE = process.env.DEVICE!;
const JETT = "add6443a-41bd-e414-f6ad-e58d267f4e95";
const SIM_TAKEN = "5f8d3a7f-467b-97f3-062c-13acf203c006";

let failed = false;
const assert = (cond: boolean, label: string, detail?: unknown) => {
  console.log(`${cond ? "✅" : "❌"} ${label}${detail !== undefined ? `  ${JSON.stringify(detail)}` : ""}`);
  if (!cond) failed = true;
};

// Pair.
const pr = await (await fetch(`${HTTP}/pair`, {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ code: CODE, device: DEVICE }),
})).json();
if (!pr.ok) { console.log("❌ pair failed", pr); process.exit(1); }
const token = pr.token as string;

// Connect + auth.
const states: any[] = [];
const ws = new WebSocket(`${WS}/agent?role=phone&device=${DEVICE}`);
await new Promise<void>((res) => ws.addEventListener("open", () => res(), { once: true }));
ws.addEventListener("message", (e) => {
  const f = JSON.parse(String(e.data));
  if (f.type === "state") states.push(f.data);
});
ws.send(JSON.stringify({ type: "phone_auth", data: { token } }));

function waitState(pred: (s: any) => boolean, timeoutMs = 8000): Promise<any> {
  return new Promise((resolve, reject) => {
    const hit = states.find(pred);
    if (hit) return resolve(hit);
    const started = Date.now();
    const iv = setInterval(() => {
      const m = states.find(pred);
      if (m) { clearInterval(iv); resolve(m); }
      else if (Date.now() - started > timeoutMs) { clearInterval(iv); reject(new Error("timeout")); }
    }, 100);
  });
}
const send = (o: unknown) => ws.send(JSON.stringify(o));

try {
  // 0) Pre-match ladder: presence/party drive distinct menus→lobby→queue→
  //    matchfound states (Phase 5). Connect is fast, so lobby onward (sim
  //    ticks ≥3) is reliably observed; menus (ticks 1-2) may precede connect.
  const lobby = await waitState((s) => s.state === "lobby", 6000);
  assert(lobby.queue_id === "competitive", "lobby: selected queue forwarded", lobby.queue_id);
  await waitState((s) => s.state === "queue", 6000);
  assert(true, "queue: matchmaking state reached");
  await waitState((s) => s.state === "matchfound", 6000);
  assert(true, "matchfound: ready-check state reached");

  // 1) Reach agent select; verify taken/teammates/owned wiring.
  const pregame = await waitState((s) => s.state === "pregame");
  assert(pregame.match_id === "sim-match", "pregame: match_id present", pregame.match_id);
  assert(pregame.map_id === "/Game/Maps/Triad/Triad", "pregame: map_id is GLZ path", pregame.map_id);
  assert(pregame.teammates.length === 3, "pregame: 3 teammates", pregame.teammates.length);
  assert(pregame.taken_agent_uuids.includes(SIM_TAKEN), "pregame: ally-locked agent is taken", pregame.taken_agent_uuids);
  assert(pregame.owned_agent_uuids.includes(SIM_TAKEN), "pregame: owned agents present");
  assert(pregame.game_locale === "vi-VN", "pregame: game locale forwarded", pregame.game_locale);
  assert(pregame.self_status === "", "pregame: self not yet selected", pregame.self_status);

  // 2) get_state returns the current snapshot on demand (always pushes a frame).
  const before = states.length;
  send({ type: "get_state" });
  await new Promise((r) => setTimeout(r, 800));
  assert(states.length > before, "get_state pushed a state frame", { before, after: states.length });

  // 3) Lock Jett → optimistic "locking" → reconciled "locked" from game truth.
  send({ type: "lock", reqId: "L1", data: { agentId: JETT } });
  const locked = await waitState((s) => s.state === "locked");
  assert(locked.prepick_status === "locked", "lock reconciled to locked", locked.prepick_status);
  assert(locked.self_agent_uuid === JETT, "locked: self_agent_uuid is Jett", locked.self_agent_uuid);
  assert(locked.self_status === "locked", "locked: self_status is locked", locked.self_status);
  const you = locked.teammates.find((t: any) => t.agent_uuid === JETT);
  assert(!!you && you.status === "locked", "our seat shows locked Jett", you);
  assert(!!you && you.self === true, "our seat is flagged self", you);
  assert(
    locked.teammates.filter((t: any) => t.self).length === 1,
    "exactly one seat is flagged self",
    locked.teammates.filter((t: any) => t.self).length,
  );

  // 4) Match starts → ingame.
  const ingame = await waitState((s) => s.state === "ingame", 8000);
  assert(ingame.state === "ingame", "transitions to ingame after lock");
} catch (e) {
  assert(false, "flow completed without timeout", String(e));
}

ws.close();
console.log(failed ? "\nFAIL" : "\nPASS");
process.exit(failed ? 1 : 0);
