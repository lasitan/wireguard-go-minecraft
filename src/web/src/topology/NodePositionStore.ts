import type { Mesh } from "../core/models";
import { state } from "../core/state";

const KEY_PREFIX = "lasitan_topo_pos:";

function storageKey(): string {
  return KEY_PREFIX + window.location.origin;
}

type PosMap = Record<string, { x: number; y: number }>;

function readRaw(): PosMap {
  try {
    const raw = localStorage.getItem(storageKey());
    if (!raw) return {};
    const parsed = JSON.parse(raw) as PosMap;
    if (!parsed || typeof parsed !== "object") return {};
    const out: PosMap = {};
    for (const [id, p] of Object.entries(parsed)) {
      if (p && typeof p.x === "number" && typeof p.y === "number" && Number.isFinite(p.x) && Number.isFinite(p.y)) {
        out[id] = { x: p.x, y: p.y };
      }
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
  const out: PosMap = {};
  for (const id of Object.keys(state.nodePositions)) {
    if (!live.has(id)) continue;
    out[id] = state.nodePositions[id];
  }
  try {
    localStorage.setItem(storageKey(), JSON.stringify(out));
  } catch {
    /* quota / private mode */
  }
}
