import type { MagnetStacks, Mesh, Node } from "../core/models";
import { vpnPrefixFromAddress } from "./SubnetGroups";

/** A node with a listen port accepts children (Master: Node.IsMagnetParent). */
export function isMother(node: Node | undefined): boolean {
  return !!node && (node.listenPort ?? 0) > 0;
}

/** Every mother a card is attached to, in link order. */
export function attachedMothers(mesh: Mesh, id: string): string[] {
  const byId = new Map(mesh.nodes.map((n) => [n.id, n]));
  if (isMother(byId.get(id))) return [];
  const out: string[] = [];
  for (const l of mesh.links || []) {
    if (l.fromNodeId === id && isMother(byId.get(l.toNodeId)) && !out.includes(l.toNodeId)) out.push(l.toNodeId);
  }
  return out;
}

/** Master's gateway for `id` into its own subnet, when it is one of the attached mothers. */
export function homeMother(mesh: Mesh, id: string, attached = attachedMothers(mesh, id)): string {
  const node = mesh.nodes.find((n) => n.id === id);
  const own = node ? vpnPrefixFromAddress(node.address) : "";
  const chosen = own ? mesh.paths?.[id]?.[own] : undefined;
  if (chosen && attached.includes(chosen)) return chosen;
  return attached[0] || "";
}

/**
 * A card stacks under its home mother (the fastest one Master picked); other
 * attachments are drawn as links.
 */
export function buildMagnetStacks(mesh: Mesh): MagnetStacks {
  const parentOf = new Map<string, string>();
  const childrenOf = new Map<string, string[]>();
  for (const n of mesh.nodes) {
    const home = homeMother(mesh, n.id);
    if (!home) continue;
    parentOf.set(n.id, home);
  }
  for (const l of mesh.links || []) {
    if (parentOf.get(l.fromNodeId) !== l.toNodeId) continue;
    const list = childrenOf.get(l.toNodeId) || [];
    if (!list.includes(l.fromNodeId)) list.push(l.fromNodeId);
    childrenOf.set(l.toNodeId, list);
  }
  return { parentOf, childrenOf };
}
