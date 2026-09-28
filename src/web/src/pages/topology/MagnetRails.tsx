import type { ReactElement } from "react";
import type { PlacedNode } from "../../core/models";
import { state } from "../../core/state";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP } from "../../topology/MagnetLayout";
import { pcbStackRouteD, pcbStackRoutePoints } from "../../topology/PcbRoute";
import { isOnline } from "../../utils/isOnline";

function railStatus(p: PlacedNode): string {
  return p.node.disabled ? "grey" : isOnline(p.node) ? "green" : "red";
}

/**
 * Horizontal joints between side-by-side cluster members: one solid line each
 * between adjacent cards (left → right).
 */
function clusterJoints(placed: PlacedNode[]): ReactElement[] {
  const groups = new Map<string, PlacedNode[]>();
  for (const p of placed) {
    const c = p.node.cluster;
    if (!c || p.node.id === state.draggingId) continue;
    groups.set(c, [...(groups.get(c) || []), p]);
  }
  const out: ReactElement[] = [];
  const midY = (CARD_TOP + CARD_BOTTOM) / 2;
  for (const [c, members] of groups) {
    members.sort((a, b) => a.x - b.x);
    for (let i = 1; i < members.length; i++) {
      const a = members[i - 1];
      const b = members[i];
      const x1 = a.x + CARD_HALF_W;
      const x2 = b.x - CARD_HALF_W;
      const y1 = a.y + midY;
      const y2 = b.y + midY;
      const status = railStatus(a) === "green" && railStatus(b) === "green" ? "green" : "red";
      out.push(
        <g key={`${c}:${a.node.id}:${b.node.id}`} className={`magnet-rail cluster-joint status-${status}`}>
          <path d={`M ${x1} ${y1} L ${x2} ${y2}`} />
          <circle cx={x1} cy={y1} r={3} />
          <circle cx={x2} cy={y2} r={3} />
        </g>,
      );
    }
  }
  return out;
}

/**
 * Short solid magnetic joints between a mother card and each card stacked below
 * it (one solid line per adjacent pair — no mesh edges inside the stack).
 */
export function MagnetRails({ placed }: { placed: PlacedNode[] }) {
  const byId = new Map(placed.map((p) => [p.node.id, p]));
  const rails: ReactElement[] = [];
  for (const [motherId, kids] of state.stacks.childrenOf) {
    let above = byId.get(motherId);
    for (const kidId of kids) {
      const kid = byId.get(kidId);
      if (!above || !kid || kidId === state.draggingId) continue;
      const y1 = above.y + CARD_BOTTOM;
      const y2 = kid.y + CARD_TOP;
      const pts = pcbStackRoutePoints(above.x, y1, kid.x, y2);
      const end = pts[pts.length - 1];
      rails.push(
        <g key={`${motherId}:${kidId}`} className={`magnet-rail status-${railStatus(kid)}`}>
          <path d={pcbStackRouteD(above.x, y1, kid.x, y2)} />
          <circle cx={above.x} cy={y1} r={3} />
          <circle cx={end.x} cy={end.y} r={3} />
        </g>,
      );
      above = kid;
    }
  }
  rails.push(...clusterJoints(placed));
  return <g className="magnet-rails">{rails}</g>;
}
