import { demoMesh, demoMeta } from "./demoData";
import { homeCam } from "../camera/CameraMath";
import { isDevPreview, TOKEN_KEY } from "./constants";
import type { Cam, Mesh, Meta, PlacedNode } from "./models";

export type AppState = {
  token: string;
  mesh: Mesh | null;
  meta: Meta | null;
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
  busy: boolean;
};

export const state: AppState = {
  token: isDevPreview ? "dev-preview" : localStorage.getItem(TOKEN_KEY) || "",
  mesh: isDevPreview ? demoMesh() : null,
  meta: isDevPreview ? demoMeta() : null,
  selectedId: null,
  drawerOpen: false,
  contentSwap: false,
  err: "",
  pollTimer: null,
  demo: isDevPreview,
  camera: homeCam(),
  placed: [],
  nodePositions: {},
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
