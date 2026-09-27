import type { Node } from "../core/models";

export function shortName(n: Node): string {
  const raw = (n.name || n.role || n.id).trim();
  return raw.length > 14 ? raw.slice(0, 12) + "…" : raw;
}
