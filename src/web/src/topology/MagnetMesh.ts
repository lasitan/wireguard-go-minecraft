import type { Link, Mesh, NodePatch } from "../core/models";
import { isMother } from "./MagnetStacks";

const KEEPALIVE = 5;

/**
 * Local mirror of Master's magnet rules (store_magnet.go), used for demo mode
 * and optimistic updates so cards snap before the PATCH round-trip.
 */
export function applyMagnetPatch(mesh: Mesh, id: string, patch: NodePatch): Mesh {
  let links: Link[] = mesh.links || [];
  const nodes = mesh.nodes.map((n) => {
    if (n.id !== id) return n;
    const next = { ...n };
    if (patch.endpoint !== undefined) next.endpoint = patch.endpoint.trim() || undefined;
    if (patch.listenPort !== undefined) {
      const port = patch.listenPort;
      next.listenPort = port > 0 ? port : undefined;
      next.role = port > 0 ? "server" : "client";
      links = links.filter((l) => (port > 0 ? l.fromNodeId !== id : l.toNodeId !== id));
    }
    return next;
  });
  const parents = patch.parentIds ?? (patch.parentId !== undefined ? [patch.parentId] : undefined);
  if (parents !== undefined) {
    const self = nodes.find((n) => n.id === id);
    links = links.filter((l) => l.fromNodeId !== id);
    if (!isMother(self)) {
      for (const pid of new Set(parents.filter(Boolean))) {
        if (isMother(nodes.find((n) => n.id === pid))) {
          links = [...links, { fromNodeId: id, toNodeId: pid, keepalive: KEEPALIVE }];
        }
      }
    }
  }
  return { ...mesh, nodes, links };
}
