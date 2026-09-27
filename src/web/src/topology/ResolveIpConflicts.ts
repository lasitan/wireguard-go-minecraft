import type { IpConflictMap, Mesh, Node } from "../core/models";
import { hostKey } from "../utils/hostOf";

/** Missing / Go zero time sorts first, same as Master. */
function changedAt(n: Node): number {
  const t = n.addressChangedAt ? Date.parse(n.addressChangedAt) : NaN;
  return Number.isFinite(t) && t > 0 ? t : -Infinity;
}

/**
 * Same VPN host IP → mutual exclusion, matching Master's ConflictLosers:
 * the node that changed its address earliest keeps the IP; later changers are
 * flagged (Master also withholds them from other peers). Ties fall back to
 * mesh order (enrolled earlier wins). Never uses lastSeen, which moves on
 * every heartbeat and would make the flag flip.
 */
export function ResolveIpConflicts(mesh: Mesh): IpConflictMap {
  const owner = new Map<string, { node: Node; idx: number }>();
  const conflicted: IpConflictMap = new Map();
  (mesh.nodes || []).forEach((n, idx) => {
    const ip = hostKey(n.address);
    if (!ip) return;
    const cur = owner.get(ip);
    if (!cur) {
      owner.set(ip, { node: n, idx });
      return;
    }
    const a = changedAt(n);
    const b = changedAt(cur.node);
    const challengerWins = a < b || (a === b && idx < cur.idx);
    if (challengerWins) {
      conflicted.set(cur.node.id, true);
      owner.set(ip, { node: n, idx });
    } else {
      conflicted.set(n.id, true);
    }
  });
  return conflicted;
}
