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
import { pcbRouteD } from "./PcbRoute";
import { vpnPrefixFromAddress } from "./SubnetGroups";

function pairKey(a: string, b: string): string {
  return a < b ? `${a}|${b}` : `${b}|${a}`;
}

function distToMaster(x: number, y: number): number {
  const dx = x - VIEW.cx;
  const dy = y - VIEW.cy;
  return dx * dx + dy * dy;
}

/** Orient so (x2,y2) is closer to Master — green dash flow moves toward Master. */
function orientTowardMaster(
  ax: number,
  ay: number,
  bx: number,
  by: number,
): { x1: number; y1: number; x2: number; y2: number } {
  if (distToMaster(ax, ay) <= distToMaster(bx, by)) {
    return { x1: bx, y1: by, x2: ax, y2: ay };
  }
  return { x1: ax, y1: ay, x2: bx, y2: by };
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

/**
 * Build display edges:
 * - solid: active link or forward between the pair
 * - gateway: cross-subnet gateway mother Master picked for a node (lowest RTT)
 * - dashed: indirect reachability (shared hub / control spoke) when no solid rule
 * Kinds are mutually exclusive per pair.
 */
export function BuildEdgeGraph(
  mesh: Mesh,
  placed: PlacedNode[],
  conflicts: IpConflictMap,
  stacks: MagnetStacks,
): TopologyEdge[] {
  // Cards in one magnet stack are drawn touching (MagnetRails); no lines between them.
  const stackOf = (id: string) => stacks.parentOf.get(id) || (stacks.childrenOf.has(id) ? id : "");
  const sameStack = (a: string, b: string) => {
    const s = stackOf(a);
    return s !== "" && s === stackOf(b);
  };
  const byId = new Map(placed.map((p) => [p.node.id, p]));
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

  const edges: TopologyEdge[] = [];
  const pushPair = (aId: string, bId: string, kind: EdgeKind) => {
    const a = byId.get(aId);
    const b = byId.get(bId);
    if (!a || !b) return;
    const o = orientTowardMaster(a.x, a.y, b.x, b.y);
    const status = edgeStatus(
      !!online.get(aId),
      !!online.get(bId),
      !!conflicts.get(aId),
      !!conflicts.get(bId),
      disabled.has(aId) || disabled.has(bId),
    );
    edges.push({
      id: `${kind}:${pairKey(aId, bId)}`,
      fromId: aId,
      toId: bId,
      ...o,
      pathD: pcbRouteD(o.x1, o.y1, o.x2, o.y2),
      kind,
      status,
      flowTowardMaster: status === "green",
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
  // Nodes of a relay-served subnet stay on Master's relay so it can deliver.
  for (const n of mesh.nodes) {
    const sub = vpnPrefixFromAddress(n.address);
    if (sub && relaySubnets.has(sub)) relayNodes.add(n.id);
  }

  for (const key of solidPairs) {
    const [a, b] = key.split("|");
    if (!sameStack(a, b)) pushPair(a, b, "solid");
  }
  for (const key of gatewayPairs) {
    const [a, b] = key.split("|");
    if (!sameStack(a, b)) pushPair(a, b, "gateway");
  }
  for (const key of indirectPairs) {
    if (solidPairs.has(key) || gatewayPairs.has(key)) continue;
    const [a, b] = key.split("|");
    if (!sameStack(a, b)) pushPair(a, b, "dashed");
  }

  // Control-plane spokes: agent ↔ Master (dashed; solid never applies to Master).
  // Attached cards share their mother's spoke. Nodes on Master's cross-subnet
  // relay get a gateway-style spoke of their own.
  for (const p of placed) {
    const onRelay = relayNodes.has(p.node.id);
    if (stacks.parentOf.has(p.node.id) && !onRelay) continue;
    const o = orientTowardMaster(p.x, p.y, VIEW.cx, VIEW.cy);
    const nodeOnline = !!online.get(p.node.id);
    const conflict = !!conflicts.get(p.node.id);
    let status: EdgeStatus = "green";
    if (disabled.has(p.node.id)) status = "grey";
    else if (conflict) status = "yellow";
    else if (!nodeOnline) status = "red";
    edges.push({
      id: `spoke:${p.node.id}`,
      fromId: p.node.id,
      toId: MASTER_ID,
      ...o,
      pathD: pcbRouteD(o.x1, o.y1, o.x2, o.y2),
      kind: onRelay ? "gateway" : "dashed",
      status,
      flowTowardMaster: status === "green",
    });
  }

  return edges;
}
