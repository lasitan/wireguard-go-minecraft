import { VIEW } from "../core/constants";
import type { Mesh } from "../core/models";
import { state } from "../core/state";

const KEY_PREFIX = "lasitan_topo_pos:";

function storageKey(): string {
  return KEY_PREFIX + window.location.origin;
}

type WorldPos = { x: number; y: number };
/** Hub-relative offsets — stable across viewport aspect / resolution changes. */
type StoredPos = { dx: number; dy: number };

type PosMap = Record<string, WorldPos>;

function isFiniteNum(n: unknown): n is number {
  return typeof n === "number" && Number.isFinite(n);
}

function toWorld(raw: unknown): WorldPos | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;
  if (isFiniteNum(o.dx) && isFiniteNum(o.dy)) {
    return { x: VIEW.cx + o.dx, y: VIEW.cy + o.dy };
  }
  // Legacy absolute SVG coords from older builds.
  if (isFiniteNum(o.x) && isFiniteNum(o.y)) {
    return { x: o.x, y: o.y };
  }
  return null;
}

function toStored(p: WorldPos): StoredPos {
  return { dx: p.x - VIEW.cx, dy: p.y - VIEW.cy };
}

function readRaw(): PosMap {
  try {
    const raw = localStorage.getItem(storageKey());
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (!parsed || typeof parsed !== "object") return {};
    const out: PosMap = {};
    for (const [id, v] of Object.entries(parsed)) {
      const p = toWorld(v);
      if (p) out[id] = p;
    }
    return out;
  } catch {
    return {};
  }
}

/** Restore saved layout; only fills ids not yet present in memory (safe during mesh poll). */
export function hydrateNodePositions(mesh: Mesh) {
  const saved = readRaw();
  for (const n of mesh.nodes || []) {
    const p = saved[n.id];
    if (!p) continue;
    if (state.nodePositions[n.id] === undefined) {
      state.nodePositions[n.id] = p;
    }
  }
}

let saveTimer: number | null = null;

/** Debounced write of free-card anchors for the current mesh. */
export function schedulePersistNodePositions() {
  if (saveTimer != null) window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(() => {
    saveTimer = null;
    flushNodePositionsToStorage(state.mesh);
  }, 280);
}

export function flushNodePositionsToStorage(mesh: Mesh | null) {
  if (!mesh) return;
  const live = new Set((mesh.nodes || []).map((n) => n.id));
  const out: Record<string, StoredPos> = {};
  for (const id of Object.keys(state.nodePositions)) {
    if (!live.has(id)) continue;
    out[id] = toStored(state.nodePositions[id]);
  }
  try {
    localStorage.setItem(storageKey(), JSON.stringify(out));
  } catch {
    /* quota / private mode */
  }
}
