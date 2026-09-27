import { VIEW } from "../core/constants";
import type { Node, PlacedNode } from "../core/models";
import { state } from "../core/state";

/** Default ring layout for a node index. */
export function defaultNodePos(index: number, total: number): { x: number; y: number } {
  const angle = -Math.PI / 2 + (2 * Math.PI * index) / Math.max(total, 1);
  return {
    x: VIEW.cx + Math.cos(angle) * VIEW.radius,
    y: VIEW.cy + Math.sin(angle) * VIEW.radius,
  };
}

/**
 * Build placed nodes, preserving user drag positions in `state.nodePositions`.
 * New nodes get a ring slot; removed ids are pruned.
 */
export function syncPlacedNodes(nodes: Node[]): PlacedNode[] {
  const live = new Set(nodes.map((n) => n.id));
  for (const id of Object.keys(state.nodePositions)) {
    if (!live.has(id)) delete state.nodePositions[id];
  }

  const n = nodes.length;
  const placed = nodes.map((node, i) => {
    let pos = state.nodePositions[node.id];
    if (!pos) {
      pos = defaultNodePos(i, n);
      state.nodePositions[node.id] = { ...pos };
    }
    return { node, x: pos.x, y: pos.y };
  });
  state.placed = placed;
  return placed;
}

export function setNodePosition(id: string, x: number, y: number) {
  state.nodePositions[id] = { x, y };
  const hit = state.placed.find((p) => p.node.id === id);
  if (hit) {
    hit.x = x;
    hit.y = y;
  }
}
