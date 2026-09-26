import { VIEW } from "../core/constants";
import type { Node, PlacedNode } from "../core/models";

export function layoutNodes(nodes: Node[]): PlacedNode[] {
  const n = nodes.length;
  return nodes.map((node, i) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * i) / Math.max(n, 1);
    return {
      node,
      x: VIEW.cx + Math.cos(angle) * VIEW.radius,
      y: VIEW.cy + Math.sin(angle) * VIEW.radius,
    };
  });
}
