import type { IpConflictMap, Mesh, Node } from "../core/models";
import { hostKey } from "../utils/hostOf";

/**
 * Same VPN host IP → mutual exclusion on the frontend.
 * The node that holds the IP first (earlier in mesh.nodes, i.e. enrolled earlier)
 * keeps running; later ones are marked yellow. Must not use lastSeen: it moves on
 * every heartbeat and would make the flag flip between nodes.
 * Does not alter backend / running tunnels.
 */
export function ResolveIpConflicts(mesh: Mesh): IpConflictMap {
  const owner = new Map<string, Node>();
  const conflicted: IpConflictMap = new Map();
  for (const n of mesh.nodes || []) {
    const ip = hostKey(n.address);
    if (!ip) continue;
    if (owner.has(ip)) conflicted.set(n.id, true);
    else owner.set(ip, n);
  }
  return conflicted;
}
