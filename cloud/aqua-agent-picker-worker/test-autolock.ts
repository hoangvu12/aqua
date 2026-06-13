// Phase 5 auto-lock test (run with: bun test-autolock.ts).
// Requires `wrangler dev` running and a FRESH Aqua.exe started with -sim, paired,
// with CODE + DEVICE passed via env. It arms the pre-pick (auto_lock + a known
// owned agent) during the pre-match window, then asserts the PC fires select+lock
// ONCE on its own when agent select opens — no lock command from the phone.
const WS = process.env.WS_BASE ?? "ws://127.0.0.1:8787";
const HTTP = process.env.HTTP_BASE ?? "http://127.0.0.1:8787";
const CODE = process.env.CODE!;
const DEVICE = process.env.DEVICE!;
const JETT = "add6443a-41bd-e414-f6ad-e58d267f4e95"; // owned in sim

let failed = false;
const assert = (cond: boolean, label: string, detail?: unknown) => {
  console.log(`${cond ? "✅" : "❌"} ${label}${detail !== undefined ? `  ${JSON.stringify(detail)}` : ""}`);
  if (!cond) failed = true;
};

const pr = await (await fetch(`${HTTP}/pair`, {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ code: CODE, device: DEVICE }),
})).json();
if (!pr.ok) { console.log("❌ pair failed", pr); process.exit(1); }
const token = pr.token as string;

const states: any[] = [];
let lockCommandsSent = 0;
const ws = new WebSocket(`${WS}/agent?role=phone&device=${DEVICE}`);
await new Promise<void>((res) => ws.addEventListener("open", () => res(), { once: true }));
ws.addEventListener("message", (e) => {
  const f = JSON.parse(String(e.data));
  if (f.type === "state") states.push(f.data);
});
const send = (o: any) => {
  if (o?.type === "lock") lockCommandsSent++;
  ws.send(JSON.stringify(o));
};

function waitState(pred: (s: any) => boolean, timeoutMs = 15000): Promise<any> {
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

ws.send(JSON.stringify({ type: "phone_auth", data: { token } }));

try {
  // Arm the pre-pick well before agent select (during menus/lobby/queue).
  await waitState((s) => ["menus", "lobby", "queue"].includes(s.state), 8000);
  send({ type: "set_config", reqId: "C1", data: { auto_lock: true, prepick_agent_uuid: JETT } });

  // Confirm the armed/auto-lock config is reflected back.
  const armed = await waitState((s) => s.auto_lock === true && s.prepick_agent_uuid === JETT, 8000);
  assert(armed.auto_lock === true, "config: auto_lock armed", armed.auto_lock);
  assert(["armed", "none"].includes(armed.prepick_status), "config: prepick shows armed pre-pregame", armed.prepick_status);

  // PC should auto-fire select+lock when pregame opens — no phone lock command.
  const locked = await waitState((s) => s.state === "locked", 15000);
  assert(locked.self_agent_uuid === JETT, "auto-lock locked our pre-pick", locked.self_agent_uuid);
  assert(locked.self_status === "locked", "auto-lock: self_status locked", locked.self_status);
  assert(locked.prepick_status === "locked", "auto-lock: prepick reconciled to locked", locked.prepick_status);
  assert(lockCommandsSent === 0, "no manual lock command was sent", lockCommandsSent);

  // Fires once: the lock should hold straight through to ingame.
  const ingame = await waitState((s) => s.state === "ingame", 8000);
  assert(ingame.state === "ingame", "transitions to ingame after auto-lock");
} catch (e) {
  assert(false, "flow completed without timeout", String(e));
}

ws.close();
console.log(failed ? "\nFAIL" : "\nPASS");
process.exit(failed ? 1 : 0);
