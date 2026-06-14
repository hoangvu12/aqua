import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AuthStatusData, Frame, GameStateMsg, ResultData } from "./types";
import { RELAY_WS_BASE, type Creds } from "./pair";
import { nextReqId } from "./utils";

/** Relay transport health (distinct from the game phase carried inside `state`). */
export type ConnStatus = "connecting" | "connected" | "reconnecting" | "unauthorized";

const MIN_BACKOFF = 500;
const MAX_BACKOFF = 8000;
const CMD_TIMEOUT = 8000;

export interface SetConfigArgs {
  enabled?: boolean;
  auto_lock?: boolean;
  prepick_agent_uuid?: string;
}

export interface Relay {
  conn: ConnStatus;
  /** Last game state pushed by the PC (null until the first frame arrives). */
  game: GameStateMsg | null;
  /** True once any state frame has been received this session. */
  gotState: boolean;
  select: (agentId: string) => Promise<ResultData>;
  lock: (agentId: string) => Promise<ResultData>;
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

/**
 * Drives the phone↔relay WebSocket: authenticates with the stored token, auto-
 * reconnects with capped backoff, and correlates select/lock/set_config results
 * by reqId. Calls onAuthInvalid (and stops retrying) when the relay rejects the
 * token (close 4003 / auth_status ok:false), e.g. after Unpair-all on the PC.
 */
export function useRelay(creds: Creds | null, onAuthInvalid: () => void): Relay {
  const [conn, setConn] = useState<ConnStatus>("connecting");
  const [game, setGame] = useState<GameStateMsg | null>(null);
  const [gotState, setGotState] = useState(false);

  const wsRef = useRef<WebSocket | null>(null);
  const pending = useRef<Map<string, Pending>>(new Map());
  const backoff = useRef(MIN_BACKOFF);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stopped = useRef(false);
  const onAuthInvalidRef = useRef(onAuthInvalid);
  onAuthInvalidRef.current = onAuthInvalid;

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
            setGame(f.data as GameStateMsg);
            setGotState(true);
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

  const select = useCallback((agentId: string) => command("select", { agentId }), [command]);
  const lock = useCallback((agentId: string) => command("lock", { agentId }), [command]);
  const setConfig = useCallback((args: SetConfigArgs) => command("set_config", args), [command]);
  const getState = useCallback(() => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "get_state" }));
  }, []);

  const party = useMemo<PartyActions>(
    () => ({
      generateCode: () => command("party_generate_code", {}),
      disableCode: () => command("party_disable_code", {}),
      joinByCode: (code: string) => command("party_join_by_code", { code }),
      leave: () => command("party_leave", {}),
      kick: (puuid: string) => command("party_kick", { puuid }),
      setAccessibility: (open: boolean) =>
        command("party_set_accessibility", { accessibility: open ? "OPEN" : "CLOSED" }),
      setQueue: (queueId: string) => command("party_set_queue", { queueId }),
      startMatchmaking: () => command("party_start_matchmaking", {}),
      stopMatchmaking: () => command("party_stop_matchmaking", {}),
    }),
    [command],
  );

  return { conn, game, gotState, select, lock, setConfig, getState, party };
}
