import type { Mesh, PlacedNode } from "../core/models";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP, stackHitBounds } from "./MagnetLayout";
import { state } from "../core/state";
import { isMother } from "./MagnetStacks";

const ZONE_PAD = 36;

export function replaceVpnRoute(routes: string[], oldPrefix: string, newPrefix: string): string[] {
  const out = routes.filter((r) => r !== oldPrefix);
  if (!out.includes(newPrefix)) out.unshift(newPrefix);
  return out;
}

/** VPN network prefix from a node address CIDR (IPv4). */
export function vpnPrefixFromAddress(address: string): string | null {
  const m = /^([^/]+)\/(\d{1,2})$/.exec(address.trim());
  if (!m) return null;
  const oct = m[1].split(".").map((x) => parseInt(x, 10));
  if (oct.length !== 4 || oct.some((n) => Number.isNaN(n) || n < 0 || n > 255)) return null;
  const bits = parseInt(m[2], 10);
  if (bits < 0 || bits > 32) return null;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  const ip =
    ((oct[0] << 24) | (oct[1] << 16) | (oct[2] << 8) | oct[3]) >>> 0;
  const net = (ip & mask) >>> 0;
  const o0 = (net >>> 24) & 255;
  const o1 = (net >>> 16) & 255;
  const o2 = (net >>> 8) & 255;
  const o3 = net & 255;
  return `${o0}.${o1}.${o2}.${o3}/${bits}`;
}

export function nodeVpnPrefix(nodeId: string, mesh: Mesh): string | null {
  const n = mesh.nodes.find((x) => x.id === nodeId);
  return n ? vpnPrefixFromAddress(n.address) : null;
}

export type SubnetZone = {
  prefix: string;
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
};

function nodeBounds(p: PlacedNode): { minX: number; minY: number; maxX: number; maxY: number } {
  const padX = CARD_HALF_W + 8;
  let minX = p.x - padX;
  let maxX = p.x + padX;
  let minY = p.y + CARD_TOP - 8;
  let maxY = p.y + CARD_BOTTOM + 8;
  if (isMother(p.node)) {
    const kids = (state.stacks.childrenOf.get(p.node.id) || []).length;
    const pos = { x: p.x, y: p.y };
    const b = stackHitBounds(pos, kids);
    minX = Math.min(minX, b.minX);
    maxX = Math.max(maxX, b.maxX);
    minY = Math.min(minY, b.minY);
    maxY = Math.max(maxY, b.maxY);
  }
  return { minX, minY, maxX, maxY };
}

/** Unique VPN prefixes among nodes; empty if fewer than two. */
export function subnetZones(mesh: Mesh, placed: PlacedNode[]): SubnetZone[] | null {
  const byPrefix = new Map<string, PlacedNode[]>();
  for (const p of placed) {
    const pfx = vpnPrefixFromAddress(p.node.address);
    if (!pfx) continue;
    const list = byPrefix.get(pfx) || [];
    list.push(p);
    byPrefix.set(pfx, list);
  }
  if (byPrefix.size < 2) return null;

  const zones: SubnetZone[] = [];
  for (const [prefix, nodes] of byPrefix) {
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const p of nodes) {
      const b = nodeBounds(p);
      minX = Math.min(minX, b.minX);
      minY = Math.min(minY, b.minY);
      maxX = Math.max(maxX, b.maxX);
      maxY = Math.max(maxY, b.maxY);
    }
    zones.push({
      prefix,
      minX: minX - ZONE_PAD,
      minY: minY - ZONE_PAD,
      maxX: maxX + ZONE_PAD,
      maxY: maxY + ZONE_PAD,
    });
  }
  return zones;
}

export function zoneAt(x: number, y: number, zones: SubnetZone[]): SubnetZone | null {
  for (const z of zones) {
    if (x >= z.minX && x <= z.maxX && y >= z.minY && y <= z.maxY) return z;
  }
  return null;
}

export function hitPlacedNode(clientX: number, clientY: number, svg: SVGSVGElement, placed: PlacedNode[]): string | null {
  const els = document.elementsFromPoint(clientX, clientY);
  for (const el of els) {
    const g = el.closest?.(".node-wrap[data-id]");
    if (g) {
      const id = g.getAttribute("data-id");
      if (id && placed.some((p) => p.node.id === id)) return id;
    }
  }
  return null;
}
