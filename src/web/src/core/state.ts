import { demoMesh, demoMeta } from "./demoData";
import { homeCam } from "../camera/CameraMath";
import { isDevPreview, TOKEN_KEY } from "./constants";
import type { Cam, MagnetStacks, Mesh, Meta, PlacedNode, UpgradeStatus, VersionInfo } from "./models";

export type AppState = {
  token: string;
  mesh: Mesh | null;
  meta: Meta | null;
  version: VersionInfo | null;
  selectedId: string | null;
  drawerOpen: boolean;
  contentSwap: boolean;
  err: string;
  pollTimer: number | null;
  demo: boolean;
  camera: Cam;
  placed: PlacedNode[];
  /** User-dragged (and initial) agent positions keyed by node id. */
  nodePositions: Record<string, { x: number; y: number }>;
  /** Positions actually drawn last frame (tween start points). */
  displayPos: Record<string, { x: number; y: number }>;
  stacks: MagnetStacks;
  /** Card currently under the pointer drag (moved past the click threshold). */
  draggingId: string | null;
  /** Mother card the dragged card would snap to on release. */
  magnetTarget: string | null;
  /** Card the dragged card would join side by side (cluster) on release. */
  clusterTarget: { id: string; side: -1 | 1 } | null;
  /** Web-triggered upgrades keyed by node id, or "master". */
  upgrades: Record<string, UpgradeStatus>;
  busy: boolean;
};

export const state: AppState = {
  token: isDevPreview ? "dev-preview" : localStorage.getItem(TOKEN_KEY) || "",
  mesh: isDevPreview ? demoMesh() : null,
  meta: isDevPreview ? demoMeta() : null,
  version: null,
  selectedId: null,
  drawerOpen: false,
  contentSwap: false,
  err: "",
  pollTimer: null,
  demo: isDevPreview,
  camera: homeCam(),
  placed: [],
  nodePositions: {},
  displayPos: {},
  stacks: { parentOf: new Map(), childrenOf: new Map() },
  draggingId: null,
  magnetTarget: null,
  clusterTarget: null,
  upgrades: {},
  busy: false,
};

const listeners = new Set<() => void>();
let notifyRaf = 0;

function flushNotify() {
  notifyRaf = 0;
  listeners.forEach((fn) => fn());
}

/**
 * Push UI re-render after mutating `state`. Coalesced to one render per
 * animation frame so drag moves and card glides never render twice a frame.
 */
export function notify() {
  if (notifyRaf) return;
  notifyRaf = requestAnimationFrame(flushNotify);
}

export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}
