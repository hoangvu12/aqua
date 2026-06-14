import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { AuthStatusData, Frame, GameStateMsg, ResultData } from "./types";
import { RELAY_WS_BASE, type Creds } from "./pair";
import { nextReqId } from "./utils";
import { fieldPatch, overlay, type Patch } from "./optimistic";

/** Relay transport health (distinct from the game phase carried inside `state`). */
export type ConnStatus = "connecting" | "connected" | "reconnecting" | "unauthorized";

const MIN_BACKOFF = 500;
const MAX_BACKOFF = 8000;
const CMD_TIMEOUT = 8000;
// How long an optimistic patch lingers unconfirmed before we give up on it and let
// pushed truth show through (guards a wrong guess / a PC that quietly disagrees).
const OPTIMISTIC_TTL = 6000;

/** The single React Query cache cell holding the pushed (then overlaid) game state. */
const GAME_KEY = ["game"] as const;

export interface SetConfigArgs {
  enabled?: boolean;
  auto_lock?: boolean;
  prepick_agent_uuid?: string;
}

export interface Relay {
  conn: ConnStatus;
  /** Last game state pushed by the PC, with optimistic overlays (null until the
   * first frame arrives). Backed by the React Query cache at GAME_KEY. */
  game: GameStateMsg | null;
  /** True once any state frame has been received this session. */
  gotState: boolean;
  select: (agentId: string) => Promise<ResultData>;
  lock: (agentId: string) => Promise<ResultData>;
  /** Dodge (quit) agent select. Reshapes the whole state back to menus, so no
   * optimistic patch — the StateScreen shows until the PC pushes the new truth. */
  dodge: () => Promise<ResultData>;
  setConfig: (args: SetConfigArgs) => Promise<ResultData>;
  getState: () => void;
  /** Party (lobby) management. Owner-only ops are also gated by the PC. */
  party: PartyActions;
}

/** Phone→PC party commands. Each resolves with the PC's ok/message result. */
export interface PartyActions {
  generateCode: () => Promise<ResultData>;
  disableCode: () => Promise<ResultData>;
  joinByCode: (code: string) => Promise<ResultData>;
  leave: () => Promise<ResultData>;
  kick: (puuid: string) => Promise<ResultData>;
  setAccessibility: (open: boolean) => Promise<ResultData>;
  setQueue: (queueId: string) => Promise<ResultData>;
  startMatchmaking: () => Promise<ResultData>;
  stopMatchmaking: () => Promise<ResultData>;
}

interface Pending {
  resolve: (r: ResultData) => void;
  timer: ReturnType<typeof setTimeout>;
}

/** Variables for the generic command mutation: the wire call plus the optimistic
 * patches to overlay while it's in flight (omitted for unpredictable commands). */
interface CommandVars {
  type: string;
  data: unknown;
  patches?: Patch[];
}

/** Thrown by the mutation when the PC reports ok:false, so React Query's onError
 * fires (and rolls back the patches). Carries the original result for the caller. */
class CommandError extends Error {
  constructor(readonly result: ResultData) {
    super(result.message);
  }
}

/**
 * Drives the phone↔relay WebSocket: authenticates with the stored token, auto-
 * reconnects with capped backoff, and correlates select/lock/set_config results
 * by reqId. Calls onAuthInvalid (and stops retrying) when the relay rejects the
 * token (close 4003 / auth_status ok:false), e.g. after Unpair-all on the PC.
 *
 * Game state lives in the React Query cache (GAME_KEY): pushed frames seed it and
 * commands are React Query mutations that optimistically patch it on `onMutate`
 * and roll back on `onError`. Because the truth is *pushed* (not refetchable) and
 * the frame right after a command is often a stale poll, a small registry of
 * in-flight patches is re-overlaid on every frame until the truth confirms them —
 * the one bit of reconciliation the push model adds on top of stock React Query.
 */
export function useRelay(creds: Creds | null, onAuthInvalid: () => void): Relay {
  const qc = useQueryClient();
  const [conn, setConn] = useState<ConnStatus>("connecting");
  const [gotState, setGotState] = useState(false);

  // The cache is populated by the WS (via setQueryData), never fetched — so the
  // query is disabled and just subscribes this component to GAME_KEY.
  const { data: game = null } = useQuery<GameStateMsg | null>({
    queryKey: GAME_KEY,
    queryFn: () => null,
    enabled: false,
    initialData: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const pending = useRef<Map<string, Pending>>(new Map());
  const backoff = useRef(MIN_BACKOFF);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stopped = useRef(false);
  const onAuthInvalidRef = useRef(onAuthInvalid);
  onAuthInvalidRef.current = onAuthInvalid;

  // Raw last-pushed truth (kept aside from the cache so we can re-overlay patches
  // onto it), plus the in-flight optimistic patches and their expiry timers.
  const rawRef = useRef<GameStateMsg | null>(null);
  const patchesRef = useRef<Map<string, Patch>>(new Map());
  const patchTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  // Re-derive the cached view = truth + still-unconfirmed patches, retiring any the
  // truth has caught up to. The one custom reconcile step (see overlay()).
  const render = useCallback(() => {
    const { view, settled } = overlay(rawRef.current, patchesRef.current.values());
    for (const key of settled) {
      const tm = patchTimers.current.get(key);
      if (tm) clearTimeout(tm);
      patchTimers.current.delete(key);
      patchesRef.current.delete(key);
    }
    qc.setQueryData(GAME_KEY, view);
  }, [qc]);
  const renderRef = useRef(render);
  renderRef.current = render;

  const addPatch = useCallback((p: Patch) => {
    const old = patchTimers.current.get(p.key);
    if (old) clearTimeout(old);
    patchesRef.current.set(p.key, p);
    patchTimers.current.set(
      p.key,
      setTimeout(() => {
        patchTimers.current.delete(p.key);
        patchesRef.current.delete(p.key);
        renderRef.current();
      }, OPTIMISTIC_TTL),
    );
    renderRef.current();
  }, []);

  const removePatch = useCallback((key: string) => {
    const tm = patchTimers.current.get(key);
    if (tm) clearTimeout(tm);
    patchTimers.current.delete(key);
    patchesRef.current.delete(key);
    renderRef.current();
  }, []);

  useEffect(() => {
    if (!creds) return;
    stopped.current = false;

    const connect = () => {
      if (stopped.current) return;
      const base =
        RELAY_WS_BASE || `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}`;
      const url = `${base}/agent?role=phone&device=${encodeURIComponent(creds.device)}`;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.addEventListener("open", () => {
        ws.send(JSON.stringify({ type: "phone_auth", data: { token: creds.token } }));
      });

      ws.addEventListener("message", (e) => {
        let f: Frame;
        try {
          f = JSON.parse(String(e.data));
        } catch {
          return;
        }
        switch (f.type) {
          case "auth_status": {
            const d = f.data as AuthStatusData;
            if (d.ok) {
              backoff.current = MIN_BACKOFF;
              setConn("connected");
            } else {
              setConn("unauthorized");
              stopped.current = true;
              onAuthInvalidRef.current();
            }
            break;
          }
          case "state":
            // New truth: stash it and re-overlay any in-flight patches onto it.
            rawRef.current = f.data as GameStateMsg;
            setGotState(true);
            renderRef.current();
            break;
          case "result": {
            if (f.reqId) {
              const p = pending.current.get(f.reqId);
              if (p) {
                clearTimeout(p.timer);
                pending.current.delete(f.reqId);
                p.resolve((f.data as ResultData) ?? { ok: false, message: "no data" });
              }
            }
            break;
          }
        }
      });

      ws.addEventListener("close", (e) => {
        wsRef.current = null;
        // 4003 = token rejected / unpaired → don't retry; force re-pair.
        if (e.code === 4003) {
          setConn("unauthorized");
          stopped.current = true;
          onAuthInvalidRef.current();
          return;
        }
        if (stopped.current) return;
        setConn("reconnecting");
        retryTimer.current = setTimeout(connect, backoff.current);
        backoff.current = Math.min(MAX_BACKOFF, backoff.current * 2);
      });
    };

    setConn("connecting");
    connect();

    return () => {
      stopped.current = true;
      if (retryTimer.current) clearTimeout(retryTimer.current);
      for (const p of pending.current.values()) clearTimeout(p.timer);
      pending.current.clear();
      wsRef.current?.close();
      wsRef.current = null;
    };
  }, [creds]);

  // Drop every outstanding patch timer on unmount.
  useEffect(() => {
    const timers = patchTimers.current;
    return () => {
      for (const tm of timers.values()) clearTimeout(tm);
      timers.clear();
    };
  }, []);

  // Low-level: send a command and resolve with the PC's result, correlated by reqId.
  const command = useCallback((type: string, data: unknown): Promise<ResultData> => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return Promise.resolve({ ok: false, message: "not connected" });
    }
    const reqId = nextReqId();
    return new Promise<ResultData>((resolve) => {
      const timer = setTimeout(() => {
        pending.current.delete(reqId);
        resolve({ ok: false, message: "timeout" });
      }, CMD_TIMEOUT);
      pending.current.set(reqId, { resolve, timer });
      ws.send(JSON.stringify({ type, reqId, data }));
    });
  }, []);

  // The one mutation behind every command: overlay the predicted patches on
  // onMutate, send the command, and roll them back on failure. Patches that
  // succeed are retired by `render` once a pushed frame confirms them.
  const { mutateAsync } = useMutation<ResultData, CommandError, CommandVars>({
    mutationFn: async ({ type, data }) => {
      const r = await command(type, data);
      if (!r.ok) throw new CommandError(r);
      return r;
    },
    onMutate: ({ patches }) => {
      patches?.forEach(addPatch);
    },
    onError: (_err, { patches }) => {
      patches?.forEach((p) => removePatch(p.key));
    },
  });

  // mutateAsync rejects on failure (so onError runs); re-shape that back into the
  // ResultData the callers expect rather than a throw.
  const runCommand = useCallback(
    (vars: CommandVars): Promise<ResultData> =>
      mutateAsync(vars).catch((e: unknown) =>
        e instanceof CommandError
          ? e.result
          : { ok: false, message: e instanceof Error ? e.message : "error" },
      ),
    [mutateAsync],
  );

  // select/lock keep their own optimism in App (selectedUuid + the hold-to-lock
  // gesture), so they carry no patches here.
  const select = useCallback(
    (agentId: string) => runCommand({ type: "select", data: { agentId } }),
    [runCommand],
  );
  const lock = useCallback(
    (agentId: string) => runCommand({ type: "lock", data: { agentId } }),
    [runCommand],
  );
  // Dodge reshapes the whole game state (back to menus) we can't predict → no
  // patch; the UI falls through to its loading/menus view on the next frame.
  const dodge = useCallback(() => runCommand({ type: "dodge", data: {} }), [runCommand]);
  const setConfig = useCallback(
    (args: SetConfigArgs) => {
      const patches: Patch[] = [];
      if (args.auto_lock !== undefined) patches.push(fieldPatch("auto_lock", args.auto_lock));
      if (args.prepick_agent_uuid !== undefined)
        patches.push(fieldPatch("prepick_agent_uuid", args.prepick_agent_uuid));
      if (args.enabled !== undefined) patches.push(fieldPatch("enabled", args.enabled));
      return runCommand({ type: "set_config", data: args, patches });
    },
    [runCommand],
  );
  const getState = useCallback(() => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "get_state" }));
  }, []);

  const party = useMemo<PartyActions>(
    () => ({
      // Code value is Riot-assigned (unpredictable) → no patch; the drawer shows a
      // spinner instead. Disabling a code is predictable, so it patches.
      generateCode: () => runCommand({ type: "party_generate_code", data: {} }),
      disableCode: () =>
        runCommand({
          type: "party_disable_code",
          data: {},
          patches: [fieldPatch("party_invite_code", "")],
        }),
      // Joining/leaving reshapes the whole party we don't yet know → loading UI.
      joinByCode: (code: string) => runCommand({ type: "party_join_by_code", data: { code } }),
      leave: () => runCommand({ type: "party_leave", data: {} }),
      kick: (puuid: string) =>
        runCommand({
          type: "party_kick",
          data: { puuid },
          patches: [
            {
              key: `kick:${puuid}`,
              apply: (g) => ({
                ...g,
                party_members: g.party_members.filter((m) => m.puuid !== puuid),
              }),
              settled: (g) => !g.party_members.some((m) => m.puuid === puuid),
            },
          ],
        }),
      setAccessibility: (open: boolean) =>
        runCommand({
          type: "party_set_accessibility",
          data: { accessibility: open ? "OPEN" : "CLOSED" },
          patches: [fieldPatch("party_accessibility", open ? "OPEN" : "CLOSED")],
        }),
      setQueue: (queueId: string) =>
        runCommand({
          type: "party_set_queue",
          data: { queueId },
          patches: [fieldPatch("queue_id", queueId)],
        }),
      // Matchmaking flips the game phase, which drives the search timer + CTA. Both
      // share one key so a quick start→cancel can't leave a stale patch behind.
      startMatchmaking: () =>
        runCommand({
          type: "party_start_matchmaking",
          data: {},
          patches: [
            {
              key: "matchmaking",
              apply: (g) => ({
                ...g,
                state: "queue",
                queue_entry_time: g.queue_entry_time || Date.now(),
              }),
              settled: (g) => g.state === "queue",
            },
          ],
        }),
      stopMatchmaking: () =>
        runCommand({
          type: "party_stop_matchmaking",
          data: {},
          patches: [
            {
              key: "matchmaking",
              apply: (g) =>
                g.state === "queue" ? { ...g, state: "lobby", queue_entry_time: 0 } : g,
              settled: (g) => g.state !== "queue",
            },
          ],
        }),
    }),
    [runCommand],
  );

  return { conn, game, gotState, select, lock, dodge, setConfig, getState, party };
}
