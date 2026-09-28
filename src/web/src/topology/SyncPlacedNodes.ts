import { VIEW } from "../core/constants";
import type { Mesh, PlacedNode } from "../core/models";
import { state } from "../core/state";
import { motherAnchor, slotPos, stackChildren } from "./MagnetLayout";
import { buildMagnetStacks, isMother } from "./MagnetStacks";
import { schedulePersistNodePositions } from "./NodePositionStore";
import { forgetTween, tweenPos } from "./PositionTween";

/** Default ring layout for a node index. */
export function defaultNodePos(index: number, total: number): { x: number; y: number } {
  const angle = -Math.PI / 2 + (2 * Math.PI * index) / Math.max(total, 1);
  return {
    x: VIEW.cx + Math.cos(angle) * VIEW.radius,
    y: VIEW.cy + Math.sin(angle) * VIEW.radius,
  };
}

function prune(live: Set<string>) {
  let removed = false;
  for (const id of Object.keys(state.nodePositions)) {
    if (!live.has(id)) {
      delete state.nodePositions[id];
      removed = true;
    }
  }
  if (removed) schedulePersistNodePositions();
  for (const id of Object.keys(state.displayPos)) {
    if (!live.has(id)) {
      delete state.displayPos[id];
      forgetTween(id);
    }
  }
}

/**
 * Build placed nodes. Free cards keep their user-dragged spot; attached cards
 * sit in slots under their mother card. Any target change glides (PositionTween).
 */
export function syncPlacedNodes(mesh: Mesh): PlacedNode[] {
  const nodes = mesh.nodes || [];
  prune(new Set(nodes.map((n) => n.id)));
  state.stacks = buildMagnetStacks(mesh);

  const n = nodes.length;
  nodes.forEach((node, i) => {
    if (!state.nodePositions[node.id]) state.nodePositions[node.id] = defaultNodePos(i, n);
  });

  const dragging = state.draggingId;
  const placed = nodes.map((node) => {
    let target = state.nodePositions[node.id];
    const parent = state.stacks.parentOf.get(node.id);
    if (parent && node.id !== dragging && state.nodePositions[parent]) {
      const kids = stackChildren(parent, dragging);
      const idx = kids.indexOf(node.id);
      target = slotPos(state.nodePositions[parent], idx, kids.length);
    } else if (isMother(node) && node.id !== dragging) {
      const kids = stackChildren(node.id, dragging);
      target = motherAnchor(state.nodePositions[node.id], kids.length);
    }
    const pos = tweenPos(node.id, target, state.displayPos[node.id], node.id === dragging);
    state.displayPos[node.id] = pos;
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
  schedulePersistNodePositions();
}
