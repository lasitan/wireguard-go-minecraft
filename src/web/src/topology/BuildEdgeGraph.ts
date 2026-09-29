import { MASTER_ID, RELAY_ID, VIEW } from "../core/constants";
import type {
  EdgeKind,
  EdgeStatus,
  IpConflictMap,
  MagnetStacks,
  Mesh,
  PlacedNode,
  TopologyEdge,
} from "../core/models";
import { isOnline } from "../utils/isOnline";
import {
  buildAgentObstacles,
  masterObstacle,
  portOnAgent,
  portOnMaster,
  routePcbBundle,
  type Rect,
  type RouteRequest,
} from "./PcbRouter";
import { pcbRouteD } from "./PcbRoute";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP } from "./MagnetLayout";
import { vpnPrefixFromAddress } from "./SubnetGroups";

function pairKey(a: string, b: string): string {
  return a < b ? `${a}|${b}` : `${b}|${a}`;
}

/** Thin obstacle covering a magnet / cluster joint so mesh traces cannot cross it. */
function railObstacle(x1: number, y1: number, x2: number, y2: number, half = 6): Rect {
  return {
    minX: Math.min(x1, x2) - half,
    maxX: Math.max(x1, x2) + half,
    minY: Math.min(y1, y2) - half,
    maxY: Math.max(y1, y2) + half,
  };
}

function magnetRailObstacles(placed: PlacedNode[], stacks: MagnetStacks): Map<string, Rect> {
  const byId = new Map(placed.map((p) => [p.node.id, p]));
  const out = new Map<string, Rect>();
  let n = 0;
  for (const [motherId, kids] of stacks.childrenOf) {
    let above = byId.get(motherId);
    for (const kidId of kids) {
      const kid = byId.get(kidId);
      if (!above || !kid) continue;
      const y1 = above.y + CARD_BOTTOM;
      const y2 = kid.y + CARD_TOP;
      out.set(`rail:${n++}`, railObstacle(above.x, y1, kid.x, y2, 7));
      above = kid;
    }
  }
  const groups = new Map<string, PlacedNode[]>();
  for (const p of placed) {
    const c = p.node.cluster;
    if (!c) continue;
    groups.set(c, [...(groups.get(c) || []), p]);
  }
  const midY = (CARD_TOP + CARD_BOTTOM) / 2;
  for (const members of groups.values()) {
    members.sort((a, b) => a.x - b.x);
    for (let i = 1; i < members.length; i++) {
      const a = members[i - 1];
      const b = members[i];
      out.set(
        `rail:${n++}`,
        railObstacle(a.x + CARD_HALF_W, a.y + midY, b.x - CARD_HALF_W, b.y + midY, 7),
      );
    }
  }
  return out;
}

function distToMaster(x: number, y: number): number {
  const dx = x - VIEW.cx;
  const dy = y - VIEW.cy;
  return dx * dx + dy * dy;
}

/** Orient so endpoint 2 is closer to Master — green dash flow moves toward Master. */
function orientTowardMaster(
  ax: number,
  ay: number,
  bx: number,
  by: number,
  aId: string,
  bId: string,
): { x1: number; y1: number; x2: number; y2: number; fromId: string; toId: string } {
  if (distToMaster(ax, ay) <= distToMaster(bx, by)) {
    return { x1: bx, y1: by, x2: ax, y2: ay, fromId: bId, toId: aId };
  }
  return { x1: ax, y1: ay, x2: bx, y2: by, fromId: aId, toId: bId };
}

function edgeStatus(
  aOnline: boolean,
  bOnline: boolean,
  aConflict: boolean,
  bConflict: boolean,
  anyDisabled = false,
): EdgeStatus {
  if (anyDisabled) return "grey";
  if (aConflict || bConflict) return "yellow";
  if (!aOnline || !bOnline) return "red";
  return "green";
}

type PendingEdge = {
  id: string;
  fromId: string;
  toId: string;
  kind: EdgeKind;
  status: EdgeStatus;
  flowTowardMaster: boolean;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
};

/** Cache PCB path strings by geometry so selection / online ticks don't re-A*. */
let cachedGeomKey = "";
let cachedPaths = new Map<string, string>();

function roundPt(n: number): number {
  return Math.round(n);
}

function geometryKey(placed: PlacedNode[], stacks: MagnetStacks, pending: PendingEdge[]): string {
  const pos = placed
    .map((p) => `${p.node.id}:${roundPt(p.x)},${roundPt(p.y)}`)
    .sort()
    .join(";");
  const stack = [...stacks.parentOf.entries()]
    .map(([c, p]) => `${c}>${p}`)
    .sort()
    .join(";");
  const ends = pending
    .map((p) => `${p.id}:${roundPt(p.x1)},${roundPt(p.y1)}-${roundPt(p.x2)},${roundPt(p.y2)}`)
    .sort()
    .join(";");
  return `${pos}|${stack}|${ends}`;
}

/**
 * Build display edges:
 * - solid: active link or forward between the pair
 * - gateway: cross-subnet gateway mother Master picked for a node (lowest RTT)
 * - dashed: indirect reachability (shared hub / control spoke) when no solid rule
 * Kinds are mutually exclusive per pair.
 *
 * Magnet-stack members (mother + adsorbed children) are omitted here — MagnetRails
 * draws a single solid joint between adjacent stacked cards only.
 *
 * Paths are PCB-routed: orthogonal, avoid agent/Master widgets, and do not cross.
 * Pass `fast` while a card is dragged / tweening so we skip A* (L-bends only).
 */
export function BuildEdgeGraph(
  mesh: Mesh,
  placed: PlacedNode[],
  conflicts: IpConflictMap,
  stacks: MagnetStacks,
  opts?: { fast?: boolean },
): TopologyEdge[] {
  // Cards in one magnet stack / cluster are drawn with MagnetRails; no mesh lines between them.
  const stackOf = (id: string) => stacks.parentOf.get(id) || (stacks.childrenOf.has(id) ? id : "");
  const sameStack = (a: string, b: string) => {
    const s = stackOf(a);
    return s !== "" && s === stackOf(b);
  };
  const byId = new Map(placed.map((p) => [p.node.id, p]));
  const sameCluster = (a: string, b: string) => {
    const ca = byId.get(a)?.node.cluster;
    const cb = byId.get(b)?.node.cluster;
    return !!ca && ca === cb;
  };
  const magnetJoined = (a: string, b: string) => sameStack(a, b) || sameCluster(a, b);
  const online = new Map(placed.map((p) => [p.node.id, isOnline(p.node)]));
  const disabled = new Set(placed.filter((p) => p.node.disabled).map((p) => p.node.id));

  const solidPairs = new Set<string>();
  for (const l of mesh.links || []) {
    solidPairs.add(pairKey(l.fromNodeId, l.toNodeId));
  }
  for (const f of mesh.forwards || []) {
    solidPairs.add(pairKey(f.nodeId, f.destNodeId));
  }

  // Agents that share a common peer (server) are indirectly reachable.
  const neighbors = new Map<string, Set<string>>();
  const touch = (a: string, b: string) => {
    if (!neighbors.has(a)) neighbors.set(a, new Set());
    if (!neighbors.has(b)) neighbors.set(b, new Set());
    neighbors.get(a)!.add(b);
    neighbors.get(b)!.add(a);
  };
  for (const l of mesh.links || []) touch(l.fromNodeId, l.toNodeId);

  const indirectPairs = new Set<string>();
  const ids = placed.map((p) => p.node.id);
  for (let i = 0; i < ids.length; i++) {
    for (let j = i + 1; j < ids.length; j++) {
      const a = ids[i];
      const b = ids[j];
      const key = pairKey(a, b);
      if (solidPairs.has(key)) continue;
      const na = neighbors.get(a);
      const nb = neighbors.get(b);
      if (!na || !nb) continue;
      for (const x of na) {
        if (nb.has(x)) {
          indirectPairs.add(key);
          break;
        }
      }
    }
  }

  const pending: PendingEdge[] = [];

  const pushAgentPair = (aId: string, bId: string, kind: EdgeKind) => {
    const a = byId.get(aId);
    const b = byId.get(bId);
    if (!a || !b) return;
    // Attach on card borders so the trace never enters a widget.
    const aPort = portOnAgent(a, b.x, b.y);
    const bPort = portOnAgent(b, a.x, a.y);
    const o = orientTowardMaster(aPort.x, aPort.y, bPort.x, bPort.y, aId, bId);
    const status = edgeStatus(
      !!online.get(aId),
      !!online.get(bId),
      !!conflicts.get(aId),
      !!conflicts.get(bId),
      disabled.has(aId) || disabled.has(bId),
    );
    pending.push({
      id: `${kind}:${pairKey(aId, bId)}`,
      fromId: o.fromId,
      toId: o.toId,
      kind,
      status,
      flowTowardMaster: status === "green",
      x1: o.x1,
      y1: o.y1,
      x2: o.x2,
      y2: o.y2,
    });
  };

  // Cross-subnet gateways Master picked (not already an explicit attachment).
  const gatewayPairs = new Set<string>();
  const relaySubnets = new Set<string>();
  const relayNodes = new Set<string>();
  for (const [nodeId, bySub] of Object.entries(mesh.paths || {})) {
    for (const [sub, gw] of Object.entries(bySub)) {
      if (gw === RELAY_ID) {
        relaySubnets.add(sub);
        relayNodes.add(nodeId);
        continue;
      }
      const key = pairKey(nodeId, gw);
      if (!solidPairs.has(key)) gatewayPairs.add(key);
    }
  }
  for (const n of mesh.nodes) {
    const sub = vpnPrefixFromAddress(n.address);
    if (sub && relaySubnets.has(sub)) relayNodes.add(n.id);
  }

  for (const key of solidPairs) {
    const [a, b] = key.split("|");
    if (!magnetJoined(a, b)) pushAgentPair(a, b, "solid");
  }
  for (const key of gatewayPairs) {
    const [a, b] = key.split("|");
    if (!magnetJoined(a, b)) pushAgentPair(a, b, "gateway");
  }
  for (const key of indirectPairs) {
    if (solidPairs.has(key) || gatewayPairs.has(key)) continue;
    const [a, b] = key.split("|");
    if (!magnetJoined(a, b)) pushAgentPair(a, b, "dashed");
  }

  // Control-plane spokes: agent ↔ Master. Attached cards share their mother's spoke.
  for (const p of placed) {
    const onRelay = relayNodes.has(p.node.id);
    if (stacks.parentOf.has(p.node.id) && !onRelay) continue;
    const masterPort = portOnMaster(p.x, p.y);
    const agentPort = portOnAgent(p, VIEW.cx, VIEW.cy);
    const o = orientTowardMaster(agentPort.x, agentPort.y, masterPort.x, masterPort.y, p.node.id, MASTER_ID);
    const nodeOnline = !!online.get(p.node.id);
    const conflict = !!conflicts.get(p.node.id);
    let status: EdgeStatus = "green";
    if (disabled.has(p.node.id)) status = "grey";
    else if (conflict) status = "yellow";
    else if (!nodeOnline) status = "red";
    pending.push({
      id: `spoke:${p.node.id}`,
      fromId: o.fromId,
      toId: o.toId,
      kind: onRelay ? "gateway" : "dashed",
      status,
      flowTowardMaster: status === "green",
      x1: o.x1,
      y1: o.y1,
      x2: o.x2,
      y2: o.y2,
    });
  }

  // Priority: solid → gateway → dashed/spoke, then longer first within kind.
  const rank = (k: EdgeKind) => (k === "solid" ? 0 : k === "gateway" ? 1 : 2);
  pending.sort((a, b) => {
    const r = rank(a.kind) - rank(b.kind);
    if (r !== 0) return r;
    return Math.hypot(b.x2 - b.x1, b.y2 - b.y1) - Math.hypot(a.x2 - a.x1, a.y2 - a.y1);
  });

  const finish = (paths: Map<string, string>): TopologyEdge[] =>
    pending.map((p) => ({
      id: p.id,
      fromId: p.fromId,
      toId: p.toId,
      x1: p.x1,
      y1: p.y1,
      x2: p.x2,
      y2: p.y2,
      pathD: paths.get(p.id) || pcbRouteD(p.x1, p.y1, p.x2, p.y2),
      kind: p.kind,
      status: p.status,
      flowTowardMaster: p.flowTowardMaster,
    }));

  if (opts?.fast) {
    return finish(
      new Map(pending.map((p) => [p.id, pcbRouteD(p.x1, p.y1, p.x2, p.y2)] as const)),
    );
  }

  const gKey = geometryKey(placed, stacks, pending);
  if (gKey === cachedGeomKey && cachedPaths.size > 0) {
    return finish(cachedPaths);
  }

  const reqs: RouteRequest[] = pending.map((p) => ({
    id: p.id,
    fromId: p.fromId,
    toId: p.toId,
    x1: p.x1,
    y1: p.y1,
    x2: p.x2,
    y2: p.y2,
  }));
  const obstacles = buildAgentObstacles(placed);
  for (const [id, r] of magnetRailObstacles(placed, stacks)) obstacles.set(id, r);
  const paths = routePcbBundle(reqs, obstacles, masterObstacle());
  cachedGeomKey = gKey;
  cachedPaths = paths;
  return finish(paths);
}
