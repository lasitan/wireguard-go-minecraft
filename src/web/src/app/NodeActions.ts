import { GetNodeStats } from "../api/GetNodeStats";
import { GetNodeTraffic } from "../api/GetNodeTraffic";
import { PatchNode } from "../api/PatchNode";
import { ReassignSubnet } from "../api/ReassignSubnet";
import { SwapNodes } from "../api/SwapNodes";
import { replaceVpnRoute, vpnPrefixFromAddress } from "../topology/SubnetGroups";
import { PutNodeForwards } from "../api/PutNodeForwards";
import { demoNodeStats, demoTraffic } from "../core/demoStats";
import type { Forward, NodePatch, NodeStats, TrafficRange, TrafficSeries } from "../core/models";
import { notify, state } from "../core/state";
import { applyMagnetPatch } from "../topology/MagnetMesh";

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
  const magnet = applyMagnetPatch({ ...mesh, nodes }, id, patch);
  state.mesh = { ...magnet, revision: mesh.revision + 1 };
}

function demoSwapAddresses(aId: string, bId: string) {
  const mesh = state.mesh;
  if (!mesh) return;
  const a = mesh.nodes.find((n) => n.id === aId);
  const b = mesh.nodes.find((n) => n.id === bId);
  if (!a || !b) return;
  const oldA = vpnPrefixFromAddress(a.address);
  const oldB = vpnPrefixFromAddress(b.address);
  const addrA = a.address;
  a.address = b.address;
  b.address = addrA;
  const now = new Date().toISOString();
  a.addressChangedAt = now;
  b.addressChangedAt = now;
  if (oldA && oldB) {
    const newA = vpnPrefixFromAddress(a.address);
    const newB = vpnPrefixFromAddress(b.address);
    if (newA) a.routes = replaceVpnRoute(a.routes ?? [], oldA, newA);
    if (newB) b.routes = replaceVpnRoute(b.routes ?? [], oldB, newB);
  }
  state.mesh = { ...mesh, revision: mesh.revision + 1 };
}

export async function swapNodeAddresses(aId: string, bId: string): Promise<void> {
  const before = state.mesh;
  if (!before) return;
  if (state.demo) {
    demoSwapAddresses(aId, bId);
    notify();
    return;
  }
  try {
    state.mesh = await SwapNodes(aId, bId);
  } catch (e) {
    state.mesh = before;
    throw e;
  } finally {
    notify();
  }
}

export async function reassignNodeSubnet(nodeId: string, prefix: string): Promise<void> {
  const before = state.mesh;
  if (!before) return;
  if (state.demo) {
    notify();
    return;
  }
  try {
    state.mesh = await ReassignSubnet(nodeId, prefix);
  } catch (e) {
    state.mesh = before;
    throw e;
  } finally {
    notify();
  }
}

/**
 * Snap a card under a single mother ("" = detach). The local mesh changes
 * first so the card glides into its slot immediately; Master's reply then
 * wins, and a failure restores the previous mesh.
 */
export function attachNode(id: string, parentId: string): Promise<void> {
  return setNodeParents(id, parentId ? [parentId] : []);
}

/** Replace the set of mother cards a card is attached to (optimistic). */
export async function setNodeParents(id: string, parentIds: string[]): Promise<void> {
  const before = state.mesh;
  if (!before) return;
  const patch: NodePatch = { parentIds };
  if (state.demo) {
    demoApplyPatch(id, patch);
    notify();
    return;
  }
  state.mesh = applyMagnetPatch(before, id, patch);
  notify();
  try {
    state.mesh = await PatchNode(id, patch);
  } catch (e) {
    state.mesh = before;
    throw e;
  } finally {
    notify();
  }
}

/** Join `peerId`'s side-by-side cluster ("" = leave), optimistic like setNodeParents. */
export async function setNodeCluster(id: string, peerId: string): Promise<void> {
  const before = state.mesh;
  if (!before) return;
  const patch: NodePatch = { clusterWith: peerId };
  if (state.demo) {
    demoApplyPatch(id, patch);
    notify();
    return;
  }
  state.mesh = applyMagnetPatch(before, id, patch);
  notify();
  try {
    state.mesh = await PatchNode(id, patch);
  } catch (e) {
    state.mesh = before;
    throw e;
  } finally {
    notify();
  }
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
