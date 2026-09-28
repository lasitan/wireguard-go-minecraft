import type { ReactElement } from "react";
import type { PlacedNode } from "../../core/models";
import { state } from "../../core/state";
import { CARD_BOTTOM, CARD_TOP } from "../../topology/MagnetLayout";
import { pcbStackRouteD, pcbStackRoutePoints } from "../../topology/PcbRoute";
import { isOnline } from "../../utils/isOnline";

/** Short magnetic joints between a mother card and each card stacked below it. */
export function MagnetRails({ placed }: { placed: PlacedNode[] }) {
  const byId = new Map(placed.map((p) => [p.node.id, p]));
  const rails: ReactElement[] = [];
  for (const [motherId, kids] of state.stacks.childrenOf) {
    let above = byId.get(motherId);
    for (const kidId of kids) {
      const kid = byId.get(kidId);
      if (!above || !kid || kidId === state.draggingId) continue;
      const status = kid.node.disabled ? "grey" : isOnline(kid.node) ? "green" : "red";
      const y1 = above.y + CARD_BOTTOM;
      const y2 = kid.y + CARD_TOP;
      const pts = pcbStackRoutePoints(above.x, y1, kid.x, y2);
      const end = pts[pts.length - 1];
      rails.push(
        <g key={`${motherId}:${kidId}`} className={`magnet-rail status-${status}`}>
          <path d={pcbStackRouteD(above.x, y1, kid.x, y2)} />
          <circle cx={above.x} cy={y1} r={3} />
          <circle cx={end.x} cy={end.y} r={3} />
        </g>,
      );
      above = kid;
    }
  }
  return <g className="magnet-rails">{rails}</g>;
}
