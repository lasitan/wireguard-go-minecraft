import type { IpConflictMap, Mesh, Node } from "../core/models";
import { hostKey } from "../utils/hostOf";

/**
 * Same VPN host IP → mutual exclusion on the frontend.
 * The earlier-seen / running node keeps green; the later-changed peer is marked yellow.
 * Does not alter backend / running tunnels.
 */
export function ResolveIpConflicts(mesh: Mesh): IpConflictMap {
  const byIp = new Map<string, Node[]>();
  for (const n of mesh.nodes || []) {
    const ip = hostKey(n.address);
    if (!ip) continue;
    const list = byIp.get(ip) || [];
    list.push(n);
    byIp.set(ip, list);
  }

  const conflicted: IpConflictMap = new Map();
  for (const group of byIp.values()) {
    if (group.length < 2) continue;
    const sorted = [...group].sort((a, b) => {
      const ta = Date.parse(a.lastSeen || "") || 0;
      const tb = Date.parse(b.lastSeen || "") || 0;
      // Earlier lastSeen ≈ already running; later change is the erroneous one.
      return ta - tb;
    });
    for (let i = 1; i < sorted.length; i++) {
      conflicted.set(sorted[i].id, true);
    }
  }
  return conflicted;
}
