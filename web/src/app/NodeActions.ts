import { GetNodeStats } from "../api/GetNodeStats";
import { GetNodeTraffic } from "../api/GetNodeTraffic";
import { PatchNode } from "../api/PatchNode";
import { PutNodeForwards } from "../api/PutNodeForwards";
import { demoNodeStats, demoTraffic } from "../core/demoStats";
import type { Forward, NodePatch, NodeStats, TrafficRange, TrafficSeries } from "../core/models";
import { notify, state } from "../core/state";

/** Mirrors Master's route normalization closely enough for demo mode. */
function demoApplyPatch(id: string, patch: NodePatch) {
  const mesh = state.mesh;
  if (!mesh) return;
  const nodes = mesh.nodes.map((n) => {
    if (n.id !== id) return n;
    const next = { ...n };
    if (patch.name !== undefined) next.name = patch.name.trim();
    if (patch.address !== undefined && patch.address !== n.address) {
      next.address = patch.address.trim();
      next.addressChangedAt = new Date().toISOString();
    }
    if (patch.enabled !== undefined) next.disabled = !patch.enabled;
    if (patch.routes !== undefined) next.routes = [...new Set(patch.routes.map((r) => r.trim()).filter(Boolean))];
    return next;
  });
  state.mesh = { ...mesh, revision: mesh.revision + 1, nodes };
}

export async function patchNode(id: string, patch: NodePatch): Promise<void> {
  if (state.demo) {
    demoApplyPatch(id, patch);
  } else {
    state.mesh = await PatchNode(id, patch);
  }
  notify();
}

export async function saveForwards(id: string, forwards: Forward[]): Promise<void> {
  const mesh = state.mesh;
  if (!mesh) return;
  const saved = state.demo ? forwards.map((f) => ({ ...f, nodeId: id })) : await PutNodeForwards(id, forwards);
  const cur = state.mesh || mesh;
  state.mesh = {
    ...cur,
    revision: cur.revision + 1,
    forwards: [...cur.forwards.filter((f) => f.nodeId !== id), ...saved],
  };
  notify();
}

export function fetchNodeStats(id: string): Promise<NodeStats> {
  if (state.demo && state.mesh) return Promise.resolve(demoNodeStats(state.mesh, id));
  return GetNodeStats(id);
}

export function fetchNodeTraffic(id: string, range: TrafficRange): Promise<TrafficSeries> {
  if (state.demo && state.mesh) return Promise.resolve(demoTraffic(state.mesh, id, range));
  return GetNodeTraffic(id, range);
}
