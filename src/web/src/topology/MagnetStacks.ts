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

/** Ids in the same cluster as `id` (itself included); just [id] when unclustered. */
export function clusterMembers(mesh: Mesh, id: string): string[] {
  const c = mesh.nodes.find((n) => n.id === id)?.cluster;
  if (!c) return [id];
  return mesh.nodes.filter((n) => n.cluster === c).map((n) => n.id);
}

/** Attached mothers plus their cluster siblings (Master serves a card from all of them). */
export function servingMothers(mesh: Mesh, id: string): string[] {
  const out: string[] = [];
  for (const m of attachedMothers(mesh, id)) {
    for (const s of clusterMembers(mesh, m)) {
      if (!out.includes(s) && isMother(mesh.nodes.find((n) => n.id === s))) out.push(s);
    }
  }
  return out;
}

/** Master's gateway for `id` into its own subnet, when it is one of the serving mothers. */
export function homeMother(mesh: Mesh, id: string, serving = servingMothers(mesh, id)): string {
  const node = mesh.nodes.find((n) => n.id === id);
  const own = node ? vpnPrefixFromAddress(node.address) : "";
  const chosen = own ? mesh.paths?.[id]?.[own] : undefined;
  if (chosen && serving.includes(chosen)) return chosen;
  return serving[0] || "";
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
    const list = childrenOf.get(home) || [];
    list.push(n.id);
    childrenOf.set(home, list);
  }
  return { parentOf, childrenOf };
}
