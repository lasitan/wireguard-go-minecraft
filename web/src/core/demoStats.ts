import type { LinkKind, Mesh, Node, NodeStats, TrafficRange, TrafficSeries } from "./models";
import { demoAgentVersion } from "./demoVersion";
import { hostKey, hostOf } from "../utils/hostOf";
import { isOnline } from "../utils/isOnline";
import { ResolveIpConflicts } from "../topology/ResolveIpConflicts";

/** Deterministic PRNG so demo curves are stable per node across renders. */
function seeded(seed: number) {
  let s = seed >>> 0 || 1;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 0x100000000;
  };
}

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

/** Typical byte rate for a node; servers carry more traffic. */
function baseRate(id: string, role: string): number {
  const r = seeded(hash(id))();
  return (role === "server" ? 900_000 : 180_000) * (0.5 + r);
}

function waveRate(id: string, role: string, t: number, phase: number): number {
  const base = baseRate(id, role);
  const slow = Math.sin(t / 47_000 + phase) * 0.35;
  const fast = Math.sin(t / 5_300 + phase * 2) * 0.15;
  const noise = (Math.random() - 0.5) * 0.25;
  return Math.max(0, base * (1 + slow + fast + noise));
}

/** Control link a demo node would use: some clients stay on legacy HTTP polling. */
export function demoLink(n: Node): LinkKind {
  if (!isOnline(n) || n.disabled) return "offline";
  return n.role === "server" || hash(n.id) % 4 !== 0 ? "ws" : "http";
}

type Acc = { at: number; rx: number; tx: number; ips: Map<string, [number, number]>; fwd: Map<string, [number, number]> };
const accs = new Map<string, Acc>();

/** Live-looking stats: rates wobble, totals keep growing between polls. */
export function demoNodeStats(mesh: Mesh, id: string): NodeStats {
  const n = mesh.nodes.find((x) => x.id === id);
  const now = Date.now();
  const empty: NodeStats = {
    nodeId: id,
    link: "offline",
    rxRate: 0,
    txRate: 0,
    totals: { rx: 0, tx: 0 },
    peers: [],
    forwards: [],
    ips: [],
  };
  if (!n) return empty;

  const rnd = seeded(hash(id));
  let acc = accs.get(id);
  const fresh = !acc;
  if (!acc) {
    acc = {
      at: now,
      rx: Math.round(rnd() * 80e9 + 2e9),
      tx: Math.round(rnd() * 40e9 + 1e9),
      ips: new Map(),
      fwd: new Map(),
    };
    accs.set(id, acc);
  }

  const live = isOnline(n) && !n.disabled;
  const rxRate = live ? waveRate(id, n.role, now, 0) : 0;
  const txRate = live ? waveRate(id, n.role, now, 1.7) * 0.6 : 0;
  const dt = Math.max(0, (now - acc.at) / 1000);
  acc.rx += rxRate * dt;
  acc.tx += txRate * dt;
  acc.at = now;

  // Real stats are keyed by VPN IP: skip address-less / disabled nodes and
  // IP-conflict losers (Master withholds them from peers).
  const losers = ResolveIpConflicts(mesh);
  const peers = mesh.nodes.filter((p) => p.id !== id && hostKey(p.address) && !p.disabled && !losers.has(p.id));
  const ips = peers.map((p, i) => {
    const share = 1 / (i + 1.6);
    const prAcc = acc!.ips.get(p.address) ?? [Math.round(acc!.rx * share * 0.4), Math.round(acc!.tx * share * 0.4)];
    const prRx = rxRate * share * 0.5;
    const prTx = txRate * share * 0.5;
    prAcc[0] += prRx * dt;
    prAcc[1] += prTx * dt;
    acc!.ips.set(p.address, prAcc);
    return {
      ip: hostOf(p.address),
      nodeId: p.id,
      name: p.name,
      rx: Math.round(prAcc[0]),
      tx: Math.round(prAcc[1]),
      rxRate: prRx,
      txRate: prTx,
      updatedAt: Math.floor(now / 1000),
    };
  });

  const forwards = mesh.forwards
    .filter((f) => f.nodeId === id)
    .map((f, i) => {
      const key = `${f.protocol} ${f.listen}`;
      // Rules added after the first sample start from zero, like a fresh listener.
      const a = acc!.fwd.get(key) ?? (fresh ? [Math.round(rnd() * 3e9), Math.round(rnd() * 9e9)] : [0, 0]);
      a[0] += rxRate * 0.1 * dt / (i + 1);
      a[1] += txRate * 0.3 * dt / (i + 1);
      acc!.fwd.set(key, a);
      return { protocol: f.protocol, listen: f.listen, rx: Math.round(a[0]), tx: Math.round(a[1]) };
    });

  const link = demoLink(n);
  return {
    nodeId: id,
    link,
    agentVersion: link === "ws" ? demoAgentVersion(id) : undefined,
    lastSeen: n.lastSeen,
    connectedAt: live ? new Date(now - (hash(id) % 86_400) * 1000).toISOString() : undefined,
    rttMs: live ? 18 + (hash(id) % 60) + Math.random() * 4 : undefined,
    publicV4: n.publicV4,
    publicV6: n.publicV6,
    geoCountry: n.geoCountry,
    geoCountryCode: n.geoCountryCode,
    rxRate,
    txRate,
    sampleAt: new Date(now).toISOString(),
    totals: { rx: Math.round(acc.rx), tx: Math.round(acc.tx) },
    peers: [],
    forwards,
    ips,
  };
}

const RANGE_SPEC: Record<TrafficRange, { span: number; step: number }> = {
  "1h": { span: 3600, step: 60 },
  "24h": { span: 86_400, step: 60 },
  "7d": { span: 7 * 86_400, step: 3600 },
  "30d": { span: 30 * 86_400, step: 3600 },
};

/** History buckets with a daily rhythm; stable per node/range. */
export function demoTraffic(mesh: Mesh, id: string, range: TrafficRange): TrafficSeries {
  const n = mesh.nodes.find((x) => x.id === id);
  const { span, step } = RANGE_SPEC[range];
  const end = Math.floor(Date.now() / 1000 / step) * step;
  const rnd = seeded(hash(id + range));
  const base = n ? baseRate(id, n.role) : 0;
  const offline = !n || !isOnline(n);
  const points = [];
  for (let ts = end - span + step; ts <= end; ts += step) {
    const hour = new Date(ts * 1000).getHours();
    const daily = 0.55 + 0.45 * Math.sin(((hour - 6) / 24) * Math.PI * 2);
    const burst = rnd() < 0.04 ? 2 + rnd() * 2 : 1;
    const k = daily * burst * (0.75 + rnd() * 0.5);
    // Offline nodes show history that stops two minutes ago.
    const alive = !offline || ts < end - 120;
    points.push({
      ts,
      rx: alive ? Math.round(base * step * k) : 0,
      tx: alive ? Math.round(base * 0.6 * step * k * (0.8 + rnd() * 0.4)) : 0,
    });
  }
  return { range, step, points };
}
