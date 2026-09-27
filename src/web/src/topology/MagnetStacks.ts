import type { MagnetStacks, Mesh, Node } from "../core/models";

/** A node with a listen port accepts children (Master: Node.IsMagnetParent). */
export function isMother(node: Node | undefined): boolean {
  return !!node && (node.listenPort ?? 0) > 0;
}

/** First outbound link to a mother is the attachment; mirrors Master's model. */
export function buildMagnetStacks(mesh: Mesh): MagnetStacks {
  const byId = new Map(mesh.nodes.map((n) => [n.id, n]));
  const parentOf = new Map<string, string>();
  const childrenOf = new Map<string, string[]>();
  for (const l of mesh.links || []) {
    if (parentOf.has(l.fromNodeId)) continue;
    const child = byId.get(l.fromNodeId);
    if (!child || isMother(child) || !isMother(byId.get(l.toNodeId))) continue;
    parentOf.set(l.fromNodeId, l.toNodeId);
    const list = childrenOf.get(l.toNodeId) || [];
    list.push(l.fromNodeId);
    childrenOf.set(l.toNodeId, list);
  }
  return { parentOf, childrenOf };
}
