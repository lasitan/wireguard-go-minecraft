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
import { notify, state } from "../core/state";
import { isOnline } from "../utils/isOnline";
import { buildAgentObstacles, masterObstacle, portOnAgent, portOnMaster, type CardSide } from "./PcbPorts";
import { routePcbBundle, type Rect, type RouteRequest } from "./PcbRouter";
import type { RouteJob, RouteResult } from "./pcbRoute.worker";
import { pcbRouteD } from "./PcbRoute";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP } from "./MagnetLayout";
import { ResolveIpConflicts } from "./ResolveIpConflicts";
import { syncPlacedNodes } from "./SyncPlacedNodes";
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

/** Card edges already taken by a magnet rail or cluster joint; traces leave elsewhere. */
function jointSides(placed: PlacedNode[], stacks: MagnetStacks): Map<string, Set<CardSide>> {
  const out = new Map<string, Set<CardSide>>();
  const add = (id: string, s: CardSide) => {
    if (!out.has(id)) out.set(id, new Set());
    out.get(id)!.add(s);
  };
  for (const [motherId, kids] of stacks.childrenOf) {
    let above = motherId;
    for (const kid of kids) {
      add(above, "bottom");
      add(kid, "top");
      above = kid;
    }
  }
  const groups = new Map<string, PlacedNode[]>();
  for (const p of placed) {
    const c = p.node.cluster;
    if (!c || stacks.parentOf.has(p.node.id)) continue;
    groups.set(c, [...(groups.get(c) || []), p]);
  }
  for (const members of groups.values()) {
    members.sort((a, b) => a.x - b.x);
    for (let i = 1; i < members.length; i++) {
      add(members[i - 1].node.id, "right");
      add(members[i].node.id, "left");
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

const STATUS_RANK: Record<EdgeStatus, number> = { green: 0, yellow: 1, red: 2, grey: 3 };

/** A merged unit edge is as healthy as its healthiest member link. */
function betterStatus(a: EdgeStatus, b: EdgeStatus): EdgeStatus {
  return STATUS_RANK[b] < STATUS_RANK[a] ? b : a;
}

/**
 * Wiring representative per card. A cluster plus every card stacked under a
 * member wires as one unit through the member closest to Master; other cards
 * represent themselves.
 */
function clusterReps(placed: PlacedNode[], stacks: MagnetStacks): (id: string) => string {
  const byId = new Map(placed.map((p) => [p.node.id, p]));
  const unitOf = (id: string): string => {
    const parent = stacks.parentOf.get(id);
    const parentCluster = parent ? byId.get(parent)?.node.cluster : "";
    return parentCluster || byId.get(id)?.node.cluster || "";
  };
  const rep = new Map<string, PlacedNode>();
  for (const p of placed) {
    const c = p.node.cluster;
    if (!c || unitOf(p.node.id) !== c) continue;
    const cur = rep.get(c);
    const d = distToMaster(p.x, p.y);
    if (!cur || d < distToMaster(cur.x, cur.y) || (d === distToMaster(cur.x, cur.y) && p.node.id < cur.node.id)) {
      rep.set(c, p);
    }
  }
  return (id) => {
    const u = unitOf(id);
    return (u && rep.get(u)?.node.id) || id;
  };
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

/** Last routed layout; selection / online ticks with the same geometry reuse it. */
let cachedGeomKey = "";
let cachedPaths = new Map<string, string>();
/** Endpoints each cached path was routed for, so unchanged edges keep their trace. */
let cachedEnds = new Map<string, string>();

function roundPt(n: number): number {
  return Math.round(n);
}

function endsKey(p: PendingEdge): string {
  return `${roundPt(p.x1)},${roundPt(p.y1)}-${roundPt(p.x2)},${roundPt(p.y2)}`;
}

/*
 * A* over the whole board takes tens of ms (more on big meshes), so it runs in
 * a worker; the frame that changes geometry draws interim paths and the routed
 * ones land on a later frame. One job in flight, newest queued job wins.
 */
type Job = RouteJob & { ends: Map<string, string> };
let worker: Worker | null | undefined;
let inflight: Job | null = null;
let queued: Job | null = null;
/** When true, worker completion does not notify (warmup before first paint). */
let suppressRouteNotify = false;
const routeWaiters = new Set<(key: string) => void>();

function routeWorker(): Worker | null {
  if (worker !== undefined) return worker;
  worker = null;
  if (typeof Worker === "undefined") return null;
  try {
    worker = new Worker(new URL("./pcbRoute.worker.ts", import.meta.url), { type: "module" });
  } catch {
    return null;
  }
  worker.onmessage = (ev: MessageEvent<RouteResult>) => {
    const done = inflight;
    inflight = null;
    if (queued) {
      startJob(queued);
      queued = null;
      return;
    }
    if (!done || done.key !== ev.data.key) return;
    cachedGeomKey = done.key;
    cachedPaths = new Map(ev.data.paths);
    cachedEnds = done.ends;
    for (const w of [...routeWaiters]) w(done.key);
    if (!suppressRouteNotify) notify();
  };
  worker.onerror = () => {
    worker?.terminate();
    worker = null;
    inflight = queued = null;
    for (const w of [...routeWaiters]) w("");
  };
  return worker;
}

function startJob(job: Job) {
  inflight = job;
  const { ends: _ends, ...msg } = job;
  routeWorker()!.postMessage(msg satisfies RouteJob);
}

function requestRoute(job: Job) {
  if (inflight?.key === job.key || queued?.key === job.key) return;
  if (inflight) queued = job;
  else startJob(job);
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
 * Prefetch PCB routes into the cache before the first Topology paint.
 * Keeps the loading screen up instead of flashing interim L-bends then re-routing.
 */
export async function warmEdgeGraph(timeoutMs = 800): Promise<void> {
  const mesh = state.mesh;
  if (!mesh) return;
  suppressRouteNotify = true;
  try {
    const placed = syncPlacedNodes(mesh);
    BuildEdgeGraph(mesh, placed, ResolveIpConflicts(mesh), state.stacks);
    if (!inflight && !queued) return;
    const want = inflight?.key || queued?.key || "";
    await new Promise<void>((resolve) => {
      const t = window.setTimeout(() => {
        routeWaiters.delete(onDone);
        resolve();
      }, timeoutMs);
      const onDone = (key: string) => {
        if (want && key && key !== want) return;
        window.clearTimeout(t);
        routeWaiters.delete(onDone);
        resolve();
      };
      routeWaiters.add(onDone);
    });
  } finally {
    suppressRouteNotify = false;
  }
}

/**
 * Build display edges:
 * - solid: active link or forward between the pair
 * - gateway: cross-subnet gateway mother Master picked for a node (lowest RTT)
 * - dashed: indirect reachability (shared hub / control spoke) when no solid rule
 * Kinds are mutually exclusive per pair.
 *
 * Magnet-stack members (mother + adsorbed children) are omitted here — MagnetRails
 * draws a single solid joint between adjacent stacked cards only. A cluster and the
 * cards stacked under its members are wired as one unit from a single representative.
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
  const repOf = clusterReps(placed, stacks);
  const taken = jointSides(placed, stacks);

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

  // Unit-level pairs (cluster → its representative); first kind wins, status is the best member pair.
  const unitPairs = new Map<string, { a: string; b: string; kind: EdgeKind; status: EdgeStatus }>();
  const pushAgentPair = (aId: string, bId: string, kind: EdgeKind) => {
    if (magnetJoined(aId, bId)) return;
    const ra = repOf(aId);
    const rb = repOf(bId);
    if (ra === rb) return;
    const status = edgeStatus(
      !!online.get(aId),
      !!online.get(bId),
      !!conflicts.get(aId),
      !!conflicts.get(bId),
      disabled.has(aId) || disabled.has(bId),
    );
    const key = pairKey(ra, rb);
    const prev = unitPairs.get(key);
    if (!prev) unitPairs.set(key, { a: ra, b: rb, kind, status });
    else if (prev.kind === kind) prev.status = betterStatus(prev.status, status);
  };

  const emitAgentPair = (aId: string, bId: string, kind: EdgeKind, status: EdgeStatus) => {
    const a = byId.get(aId);
    const b = byId.get(bId);
    if (!a || !b) return;
    // Attach on card borders so the trace never enters a widget.
    const aPort = portOnAgent(a, b.x, b.y, taken.get(aId));
    const bPort = portOnAgent(b, a.x, a.y, taken.get(bId));
    const o = orientTowardMaster(aPort.x, aPort.y, bPort.x, bPort.y, aId, bId);
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
    pushAgentPair(a, b, "solid");
  }
  for (const key of gatewayPairs) {
    const [a, b] = key.split("|");
    pushAgentPair(a, b, "gateway");
  }
  for (const key of indirectPairs) {
    if (solidPairs.has(key) || gatewayPairs.has(key)) continue;
    const [a, b] = key.split("|");
    pushAgentPair(a, b, "dashed");
  }
  for (const e of unitPairs.values()) emitAgentPair(e.a, e.b, e.kind, e.status);

  // Control-plane spokes: agent ↔ Master. Attached cards share their mother's
  // spoke; a cluster (with its stacked cards) shares one from its representative.
  const spokes = new Map<string, { status: EdgeStatus; relay: boolean }>();
  for (const p of placed) {
    const id = p.node.id;
    const onRelay = relayNodes.has(id);
    if (stacks.parentOf.has(id) && !onRelay) continue;
    let status: EdgeStatus = "green";
    if (disabled.has(id)) status = "grey";
    else if (conflicts.get(id)) status = "yellow";
    else if (!online.get(id)) status = "red";
    const rep = repOf(id);
    const prev = spokes.get(rep);
    spokes.set(rep, prev ? { status: betterStatus(prev.status, status), relay: prev.relay || onRelay } : { status, relay: onRelay });
  }
  for (const [id, s] of spokes) {
    const p = byId.get(id);
    if (!p) continue;
    const masterPort = portOnMaster(p.x, p.y);
    const agentPort = portOnAgent(p, VIEW.cx, VIEW.cy, taken.get(id));
    const o = orientTowardMaster(agentPort.x, agentPort.y, masterPort.x, masterPort.y, id, MASTER_ID);
    pending.push({
      id: `spoke:${id}`,
      fromId: o.fromId,
      toId: o.toId,
      kind: s.relay ? "gateway" : "dashed",
      status: s.status,
      flowTowardMaster: s.status === "green",
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

  const finish = (pathOf: (p: PendingEdge) => string | undefined): TopologyEdge[] =>
    pending.map((p) => ({
      id: p.id,
      fromId: p.fromId,
      toId: p.toId,
      x1: p.x1,
      y1: p.y1,
      x2: p.x2,
      y2: p.y2,
      pathD: pathOf(p) || pcbRouteD(p.x1, p.y1, p.x2, p.y2),
      kind: p.kind,
      status: p.status,
      flowTowardMaster: p.flowTowardMaster,
    }));
  // Until routed: edges whose ends did not move keep their trace, the rest bend.
  const interim = () =>
    finish((p) => (cachedEnds.get(p.id) === endsKey(p) ? cachedPaths.get(p.id) : undefined));

  // Drag / glide: geometry changes every frame, routing would never catch up.
  if (opts?.fast) return interim();

  const gKey = geometryKey(placed, stacks, pending);
  if (gKey === cachedGeomKey) return finish((p) => cachedPaths.get(p.id));

  const requests: RouteRequest[] = pending.map((p) => ({
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
  const ends = new Map(pending.map((p) => [p.id, endsKey(p)] as const));

  if (routeWorker()) {
    requestRoute({ key: gKey, requests, obstacles: [...obstacles], master: masterObstacle(), ends });
    return interim();
  }
  cachedGeomKey = gKey;
  cachedPaths = routePcbBundle(requests, obstacles, masterObstacle());
  cachedEnds = ends;
  return finish((p) => cachedPaths.get(p.id));
}
