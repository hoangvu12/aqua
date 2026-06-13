// Local mock relay for UI development (run: `bun run mock`).
//
// Serves the built SPA from ./dist and speaks the real Aqua relay protocol
// ({type,reqId?,data} envelope over WS, POST /pair), with a scripted game
// timeline that mirrors pc/internal/picker/sim.go. Lets the phone UI be exercised
// end-to-end without wrangler + Aqua.exe. NOT used in production (the Worker is).
//
// Open: http://127.0.0.1:9912/?code=DEV12345&device=devbox

const PORT = 9912;
const DIST = new URL("./dist/", import.meta.url);

// Real agent UUIDs so the catalog (valorant-api) renders actual art.
const JETT = "add6443a-41bd-e414-f6ad-e58d267f4e95";
const SAGE = "569fdd95-4d10-43ab-ca70-79becc718b46"; // ally-locked → taken
const PHOENIX = "eb93336a-449b-9c1b-0a54-a891f7921d69"; // ally selected (hovering)
const OWNED = [
  JETT, SAGE, PHOENIX,
  "320b2a48-4d9b-a075-30f1-1f93a9b638fa", // Sova
  "9f0d8ba9-4140-b941-57d3-a7ad57c6b417", // Brimstone
  "8e253930-4c05-31dd-1b6c-968525494517", // Omen
  "1e58de9c-4950-5125-93e9-a0aee9f98746", // Killjoy
  "117ed9e3-49f3-6512-3ccf-0cada7e3823b", // Cypher
  "a3bfb853-43b2-7238-a4f1-ad90e9e46bce", // Reyna
  "f94c3b30-42be-e959-889c-5aa313dba261", // Raze
];

interface Session {
  selfAgent: string;
  selfStatus: "" | "selected" | "locked";
  autoLock: boolean;
  enabled: boolean;
  prepick: string;
  phase: "menus" | "lobby" | "queue" | "matchfound" | "pregame" | "locked" | "ingame";
}

// Tracker row helper (mirrors riot.PlayerStats). Tier numbers follow
// valorant-api competitivetiers (Iron 1 = 3 … Radiant = 27).
function stat(
  name: string,
  tier: number,
  peak: number,
  kd: number,
  adr: number,
  hs: number,
  win: number,
  recent: boolean[],
) {
  return {
    puuid: "mock-" + name,
    name,
    tier,
    rr: 47,
    peak_tier: peak,
    matches: recent.length,
    wins: recent.filter(Boolean).length,
    win_pct: win,
    kd,
    adr,
    hs_pct: hs,
    recent,
  };
}

// Distinct agent UUIDs (valorant-api) so every portrait renders in preview.
const SOVA = "320b2a48-4d9b-a075-30f1-1f93a9b638fa";
const BRIMSTONE = "9f0d8ba9-4140-b941-57d3-a7ad57c6b417";
const OMEN = "8e253930-4c05-31dd-1b6c-968525494517";
const KILLJOY = "1e58de9c-4950-5125-93e9-a0aee9f98746";
const CYPHER = "117ed9e3-49f3-6512-3ccf-0cada7e3823b";
const REYNA = "a3bfb853-43b2-7238-a4f1-ad90e9e46bce";
const RAZE = "f94c3b30-42be-e959-889c-5aa313dba261";

// A fixed 10-player live match (5 ally + 5 enemy) for the ingame scoreboard.
const SCOREBOARD = [
  { name: "You", agent_uuid: JETT, team: "ally", self: true,
    stats: stat("You", 19, 24, 1.21, 158, 28.0, 52, [true, false, true, true, false]) },
  { name: "wazuu#1406", agent_uuid: REYNA, team: "ally", self: false,
    stats: stat("wazuu#1406", 19, 20, 1.46, 194, 22.0, 45, [true, true, false, true, true]) },
  { name: "BrimstonMimstone#NA1", agent_uuid: BRIMSTONE, team: "ally", self: false,
    stats: stat("BrimstonMimstone#NA1", 14, 16, 0.57, 96, 9.1, 36, [false, false, true, false, false]) },
  { name: "PostBTW#EUW", agent_uuid: SOVA, team: "ally", self: false,
    stats: stat("PostBTW#EUW", 10, 12, 0.8, 144, 33.3, 0, [false, false]) },
  { name: "penna#777", agent_uuid: SAGE, team: "ally", self: false,
    stats: stat("penna#777", 6, 9, 0.25, 68, 12.5, 0, [false, false]) },
  { name: "ErSupremoLaziale#EU", agent_uuid: PHOENIX, team: "enemy", self: false,
    stats: stat("ErSupremoLaziale#EU", 15, 20, 2.18, 207, 23.2, 100, [true, true, true]) },
  { name: "Sykkuno#0001", agent_uuid: OMEN, team: "enemy", self: false,
    stats: stat("Sykkuno#0001", 21, 22, 1.33, 172, 19.4, 58, [true, false, true, true, true]) },
  { name: "miyu#vn2", agent_uuid: KILLJOY, team: "enemy", self: false,
    stats: stat("miyu#vn2", 17, 18, 0.94, 131, 15.0, 40, [false, true, false, false, true]) },
  { name: "Tenz#TENZ", agent_uuid: RAZE, team: "enemy", self: false,
    stats: stat("Tenz#TENZ", 27, 27, 1.88, 221, 31.7, 70, [true, true, false, true, true]) },
  { name: "noob#123", agent_uuid: CYPHER, team: "enemy", self: false,
    stats: stat("noob#123", 0, 0, 0.61, 88, 11.2, 25, [false, false, true, false]) },
];

function makeState(s: Session) {
  const inSelect = s.phase === "pregame" || s.phase === "locked";
  const hasQueue = inSelect || s.phase === "lobby" || s.phase === "queue" || s.phase === "matchfound";
  return {
    type: "state",
    data: {
      state: s.phase,
      match_id: inSelect ? "mock-match" : "",
      map_id: inSelect ? "/Game/Maps/Triad/Triad" : "", // Haven
      queue_id: hasQueue ? "competitive" : "",
      prepick_agent_uuid: s.prepick,
      auto_lock: s.autoLock,
      enabled: s.enabled,
      phase_time_remaining_ns: inSelect ? 45_000_000_000 : 0,
      owned_agent_uuids: OWNED,
      taken_agent_uuids: inSelect ? [SAGE] : [],
      prepick_status: s.selfStatus === "locked" ? "locked" : "none",
      game_locale: "en-US",
      teammates: inSelect
        ? [
            { name: "You", agent_uuid: s.selfAgent, status: s.selfStatus, self: true,
              stats: stat("You", 19, 24, 1.21, 158, 28.0, 52, [true, false, true, true, false]) },
            { name: "wazuu#1406", agent_uuid: SAGE, status: "locked", self: false,
              stats: stat("wazuu#1406", 19, 20, 1.46, 194, 22.0, 45, [true, true, false, true, true]) },
            { name: "Nova#NOVA", agent_uuid: PHOENIX, status: "selected", self: false,
              stats: stat("Nova#NOVA", 14, 16, 0.57, 96, 9.1, 36, [false, false, true, false, false]) },
            { name: "K2#000", agent_uuid: "", status: "", self: false, stats: null },
            { name: "Echo#001", agent_uuid: "", status: "", self: false, stats: null },
          ]
        : [],
      match_players: s.phase === "ingame" ? SCOREBOARD : [],
      self_agent_uuid: s.selfAgent,
      self_status: s.selfStatus,
    },
  };
}

const server = Bun.serve<{ s: Session }, undefined>({
  port: PORT,
  async fetch(req, srv) {
    const url = new URL(req.url);

    // Allow the Vite dev origin (different port) to call /pair cross-origin.
    const cors = {
      "access-control-allow-origin": req.headers.get("origin") ?? "*",
      "access-control-allow-methods": "POST, OPTIONS",
      "access-control-allow-headers": "content-type",
    };
    if (req.method === "OPTIONS") return new Response(null, { status: 204, headers: cors });

    // The production Worker mirrors the valorant-api catalog at same-origin /api;
    // the mock proxies it so the built (PROD) bundle can load agents, maps, and
    // competitive-tier rank icons without wrangler.
    if (url.pathname.startsWith("/api/")) {
      const upstream = "https://valorant-api.com/v1/" + url.pathname.slice(5) + url.search;
      const r = await fetch(upstream);
      return new Response(r.body, {
        status: r.status,
        headers: { "content-type": r.headers.get("content-type") ?? "application/json" },
      });
    }

    if (url.pathname === "/pair" && req.method === "POST") {
      return Response.json({ ok: true, token: "mock-token" }, { headers: cors });
    }

    if (url.pathname === "/agent") {
      // MOCK_PHASE pins the starting phase (e.g. `MOCK_PHASE=ingame`) so a single
      // state can be previewed directly; unset walks the normal ladder.
      const start = (Bun.env.MOCK_PHASE as Session["phase"]) || "menus";
      const ok = srv.upgrade(req, {
        data: {
          s: { selfAgent: "", selfStatus: "", autoLock: false, enabled: true, prepick: "", phase: start },
        },
      });
      return ok ? undefined : new Response("upgrade failed", { status: 426 });
    }

    // Static SPA assets with single-page fallback.
    let path = url.pathname === "/" ? "/index.html" : url.pathname;
    let file = Bun.file(new URL("." + path, DIST));
    if (!(await file.exists())) file = Bun.file(new URL("./index.html", DIST));
    return new Response(file);
  },
  websocket: {
    open() {
      // Wait for phone_auth before sending anything.
    },
    message(ws, raw) {
      let f: any;
      try {
        f = JSON.parse(String(raw));
      } catch {
        return;
      }
      const sess = ws.data.s;
      const send = (o: unknown) => ws.send(JSON.stringify(o));
      const pushState = () => send(makeState(sess));
      const result = (reqId: string | undefined, ok: boolean, message: string) =>
        send({ type: "result", reqId, data: { ok, message } });

      switch (f.type) {
        case "phone_auth": {
          send({ type: "auth_status", data: { ok: true, message: "phone authenticated (mock)" } });
          pushState(); // initial phase (menus, or MOCK_PHASE)
          if (Bun.env.MOCK_PHASE) break; // pinned phase → don't walk the ladder
          // Walk the pre-match ladder, then open agent select (mirrors sim.go).
          const ladder: Session["phase"][] = ["lobby", "queue", "matchfound", "pregame"];
          ladder.forEach((phase, i) => {
            setTimeout(() => {
              sess.phase = phase;
              pushState();
            }, 1200 * (i + 1));
          });
          break;
        }

        case "get_state":
          pushState();
          break;

        case "select":
          sess.selfAgent = f.data?.agentId ?? "";
          sess.selfStatus = "selected";
          result(f.reqId, true, "selected");
          pushState();
          break;

        case "lock":
          sess.selfAgent = f.data?.agentId ?? "";
          result(f.reqId, true, "locking");
          // Reconcile to locked on the "next poll", like the real picker.
          setTimeout(() => {
            sess.selfStatus = "locked";
            sess.phase = "locked";
            pushState();
          }, 700);
          setTimeout(() => {
            sess.phase = "ingame";
            pushState();
          }, 4000);
          break;

        case "set_config":
          if (typeof f.data?.enabled === "boolean") sess.enabled = f.data.enabled;
          if (typeof f.data?.auto_lock === "boolean") sess.autoLock = f.data.auto_lock;
          if (typeof f.data?.prepick_agent_uuid === "string") sess.prepick = f.data.prepick_agent_uuid;
          result(f.reqId, true, "config updated");
          pushState();
          break;
      }
    },
  },
});

console.log(`mock relay on http://127.0.0.1:${server.port}`);
console.log(`open http://127.0.0.1:${server.port}/?code=DEV12345&device=devbox`);
