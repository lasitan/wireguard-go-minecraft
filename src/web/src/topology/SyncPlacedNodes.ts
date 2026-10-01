import { VIEW } from "../core/constants";
import type { Mesh, Node, PlacedNode } from "../core/models";
import { state } from "../core/state";
import { CLUSTER_PITCH, clusterRowShift, motherAnchor, slotPos, stackChildren } from "./MagnetLayout";
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
 * Cluster members form one row: the leftmost stored card anchors it and the
 * rest follow at CLUSTER_PITCH in stored x order. Positions are per-browser
 * while clusters live on Master, so the row is re-derived instead of trusted.
 * A row whose member is being dragged is left alone until the drop.
 */
function alignClusterRows(nodes: Node[], dragging: string | null) {
  const rows = new Map<string, string[]>();
  for (const n of nodes) {
    if (!n.cluster || state.stacks.parentOf.has(n.id)) continue;
    rows.set(n.cluster, [...(rows.get(n.cluster) || []), n.id]);
  }
  let moved = false;
  for (const ids of rows.values()) {
    if (ids.length < 2 || (dragging && ids.includes(dragging))) continue;
    const members = ids
      .map((id) => ({ id, p: state.nodePositions[id] }))
      .sort((a, b) => a.p.x - b.p.x || (a.id < b.id ? -1 : 1));
    const { x, y } = members[0].p;
    members.forEach((m, i) => {
      const tx = x + i * CLUSTER_PITCH;
      if (Math.abs(m.p.x - tx) > 0.5 || Math.abs(m.p.y - y) > 0.5) {
        state.nodePositions[m.id] = { x: tx, y };
        moved = true;
      }
    });
  }
  if (moved) schedulePersistNodePositions();
}

/** Per-cluster render shift that keeps each row clear of Master as one piece. */
function clusterRowShifts(nodes: Node[], dragging: string | null): Map<string, number> {
  const rows = new Map<string, { pos: { x: number; y: number }; mother: boolean; slots: number }[]>();
  for (const n of nodes) {
    if (!n.cluster || n.id === dragging || state.stacks.parentOf.has(n.id)) continue;
    const m = { pos: state.nodePositions[n.id], mother: isMother(n), slots: stackChildren(n.id, dragging).length };
    rows.set(n.cluster, [...(rows.get(n.cluster) || []), m]);
  }
  const out = new Map<string, number>();
  for (const [c, members] of rows) {
    if (members.length < 2) continue;
    const s = clusterRowShift(members);
    if (s) out.set(c, s);
  }
  return out;
}

/**
 * Build placed nodes. Free cards keep their user-dragged spot; attached cards
 * sit in slots under their mother card; cluster members line up in a row.
 * Any target change glides (PositionTween).
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
  alignClusterRows(nodes, dragging);
  const byId = new Map(nodes.map((nd) => [nd.id, nd]));
  const rowShift = clusterRowShifts(nodes, dragging);
  const anchorOf = (id: string) => {
    const p = state.nodePositions[id];
    const c = byId.get(id)?.cluster;
    const s = c && id !== dragging && !state.stacks.parentOf.has(id) ? rowShift.get(c) || 0 : 0;
    return s ? { x: p.x + s, y: p.y } : p;
  };
  const placed = nodes.map((node) => {
    let target = state.nodePositions[node.id];
    const parent = state.stacks.parentOf.get(node.id);
    if (parent && node.id !== dragging && state.nodePositions[parent]) {
      const kids = stackChildren(parent, dragging);
      const idx = kids.indexOf(node.id);
      target = slotPos(anchorOf(parent), idx, kids.length);
    } else if (isMother(node) && node.id !== dragging) {
      const kids = stackChildren(node.id, dragging);
      target = motherAnchor(anchorOf(node.id), kids.length);
    } else if (node.id !== dragging) {
      target = anchorOf(node.id);
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
