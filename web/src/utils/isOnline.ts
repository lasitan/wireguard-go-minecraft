import { ONLINE_MS } from "../core/constants";
import type { Node } from "../core/models";

export function isOnline(n: Node): boolean {
  if (!n.lastSeen) return false;
  const t = Date.parse(n.lastSeen);
  if (Number.isNaN(t)) return false;
  return Date.now() - t < ONLINE_MS;
}
