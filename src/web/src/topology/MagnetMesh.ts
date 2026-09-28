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
  const setParents = (nodeId: string, parents: string[]) => {
    const self = nodes.find((n) => n.id === nodeId);
    links = links.filter((l) => l.fromNodeId !== nodeId);
    if (isMother(self)) return;
    for (const pid of new Set(parents.filter(Boolean))) {
      if (pid !== nodeId && isMother(nodes.find((n) => n.id === pid))) {
        links = [...links, { fromNodeId: nodeId, toNodeId: pid, keepalive: KEEPALIVE }];
      }
    }
  };
  const parents = patch.parentIds ?? (patch.parentId !== undefined ? [patch.parentId] : undefined);
  if (parents !== undefined) setParents(id, parents);
  if (patch.clusterWith !== undefined) {
    const idx = nodes.findIndex((n) => n.id === id);
    const peerIdx = nodes.findIndex((n) => n.id === patch.clusterWith);
    if (idx >= 0 && !patch.clusterWith) {
      nodes[idx] = { ...nodes[idx], cluster: undefined };
    } else if (idx >= 0 && peerIdx >= 0 && peerIdx !== idx) {
      const peer = nodes[peerIdx];
      const cluster = peer.cluster || `c-local-${peer.id.slice(0, 8)}`;
      nodes[peerIdx] = { ...peer, cluster };
      nodes[idx] = { ...nodes[idx], cluster, listenPort: peer.listenPort, role: peer.role };
      setParents(
        id,
        links.filter((l) => l.fromNodeId === peer.id).map((l) => l.toNodeId),
      );
    }
    const count = new Map<string, number>();
    for (const n of nodes) if (n.cluster) count.set(n.cluster, (count.get(n.cluster) || 0) + 1);
    nodes.forEach((n, i) => {
      if (n.cluster && (count.get(n.cluster) || 0) < 2) nodes[i] = { ...n, cluster: undefined };
    });
  }
  return { ...mesh, nodes, links };
}
