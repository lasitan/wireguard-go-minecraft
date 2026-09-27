import { demoMesh, demoMeta } from "./demoData";
import { homeCam } from "../camera/CameraMath";
import { isDevPreview, TOKEN_KEY } from "./constants";
import type { Cam, MagnetStacks, Mesh, Meta, PlacedNode, VersionInfo } from "./models";

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
  busy: false,
};

const listeners = new Set<() => void>();

/** Push UI re-render after mutating `state` (not every camera RAF frame). */
export function notify() {
  listeners.forEach((fn) => fn());
}

export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}
