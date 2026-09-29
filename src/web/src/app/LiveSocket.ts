import {
  decodeError,
  decodeFrame,
  decodeJSON,
  decodePresence,
  encodeFrame,
  encodeString,
  ErrCodeAuth,
  MsgType,
  type Presence,
} from "../api/uiWire";
import type { Mesh, Meta, NodeStats, VersionInfo } from "../core/models";
import { notify, state } from "../core/state";
import { isOnline } from "../utils/isOnline";

const PING_MS = 20_000;
const RETRY_MIN_MS = 1000;
const RETRY_MAX_MS = 15_000;

type Handlers = {
  mesh: (m: Mesh) => void;
  authFailed: (message: string) => void;
};

let ws: WebSocket | null = null;
let handlers: Handlers | null = null;
let retryMs = RETRY_MIN_MS;
let retryTimer: number | null = null;
let pingTimer: number | null = null;
let seq = 0;
let subscribed = "";
const statsWatchers = new Map<string, Set<(s: NodeStats) => void>>();

/** True while the push channel is up; HTTP polls stand down meanwhile. */
export function isLive(): boolean {
  return state.live;
}

export function startLive(h: Handlers) {
  handlers = h;
  if (ws || state.demo || !state.token) return;
  connect();
}

export function stopLive() {
  handlers = null;
  if (retryTimer != null) clearTimeout(retryTimer);
  retryTimer = null;
  const s = ws;
  ws = null;
  s?.close();
  setLive(false);
}

/** Receive pushed stats for `id` while the returned disposer is not called. */
export function watchNodeStats(id: string, fn: (s: NodeStats) => void): () => void {
  let set = statsWatchers.get(id);
  if (!set) statsWatchers.set(id, (set = new Set()));
  set.add(fn);
  syncSubscription(id);
  return () => {
    set!.delete(fn);
    if (!set!.size) statsWatchers.delete(id);
    if (subscribed === id) syncSubscription(statsWatchers.keys().next().value ?? "");
  };
}

function syncSubscription(id: string) {
  subscribed = id;
  send(MsgType.UISubscribe, encodeString(id));
}

function send(type: number, payload?: Uint8Array) {
  if (ws?.readyState === WebSocket.OPEN && state.live) ws.send(encodeFrame(type, ++seq, payload));
}

function setLive(v: boolean) {
  if (state.live === v) return;
  state.live = v;
  if (pingTimer != null) clearInterval(pingTimer);
  pingTimer = v ? window.setInterval(() => send(MsgType.Ping), PING_MS) : null;
  notify();
}

function connect() {
  const url = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/ui/ws`;
  const s = new WebSocket(url);
  s.binaryType = "arraybuffer";
  ws = s;
  s.onopen = () => s.send(encodeFrame(MsgType.UIHello, ++seq, encodeString(state.token)));
  s.onmessage = (ev) => {
    if (ws !== s || !(ev.data instanceof ArrayBuffer)) return;
    try {
      onFrame(ev.data);
    } catch (e) {
      console.warn("ui ws:", e);
    }
  };
  s.onclose = () => {
    if (ws !== s) return;
    ws = null;
    setLive(false);
    scheduleReconnect();
  };
}

function scheduleReconnect() {
  if (!handlers || retryTimer != null) return;
  retryTimer = window.setTimeout(() => {
    retryTimer = null;
    if (handlers && !ws && state.token) connect();
  }, retryMs);
  retryMs = Math.min(retryMs * 2, RETRY_MAX_MS);
}

function onFrame(buf: ArrayBuffer) {
  const f = decodeFrame(buf);
  switch (f.type) {
    case MsgType.UIHelloAck:
      retryMs = RETRY_MIN_MS;
      setLive(true);
      if (subscribed) syncSubscription(subscribed);
      return;
    case MsgType.UIError: {
      const e = decodeError(f.payload);
      if (e.code === ErrCodeAuth) {
        const h = handlers;
        stopLive();
        h?.authFailed(e.message);
      }
      return;
    }
    case MsgType.UIMesh:
      handlers?.mesh(decodeJSON<Mesh>(f.payload));
      return;
    case MsgType.UIMeta:
      state.meta = decodeJSON<Meta>(f.payload);
      notify();
      return;
    case MsgType.UIVersion:
      state.version = decodeJSON<VersionInfo>(f.payload);
      notify();
      return;
    case MsgType.UIPresence:
      applyPresence(decodePresence(f.payload));
      return;
    case MsgType.UINodeStats: {
      const st = decodeJSON<NodeStats>(f.payload);
      statsWatchers.get(st.nodeId)?.forEach((fn) => fn(st));
      return;
    }
  }
}

/**
 * Folds heartbeat times into the mesh in place and re-renders only when a
 * node's online state flips; traffic rates land in state.presence.
 */
function applyPresence(p: Presence) {
  const presence: typeof state.presence = {};
  p.nodes.forEach((n) => (presence[n.nodeId] = n));
  state.presence = presence;
  const mesh = state.mesh;
  if (!mesh) return;
  let flipped = false;
  for (const node of mesh.nodes || []) {
    const e = presence[node.id];
    if (e?.lastSeenMs) node.lastSeen = new Date(e.lastSeenMs).toISOString();
    const on = isOnline(node);
    if (lastOnline.get(node.id) !== on) flipped = true;
    lastOnline.set(node.id, on);
  }
  if (flipped) notify();
}

const lastOnline = new Map<string, boolean>();
