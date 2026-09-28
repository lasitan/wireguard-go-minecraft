import { state } from "../../core/state";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP, motherAnchor, slotPos, stackChildren } from "../../topology/MagnetLayout";
import { pcbStackRouteD } from "../../topology/PcbRoute";

/** Dashed slot under the hovered mother card: where the dragged card will land. */
export function MagnetGhost() {
  const target = state.magnetTarget;
  const dragging = state.draggingId;
  const mother = target ? state.nodePositions[target] : undefined;
  if (!target || !dragging || !mother) return null;
  const n = stackChildren(target, dragging).length;
  const slot = slotPos(mother, n, n);
  const above = n === 0 ? motherAnchor(mother, n) : slotPos(mother, n - 1, n);
  return (
    <g key={target} className="magnet-ghost">
      <path
        className="magnet-ghost-rail"
        d={pcbStackRouteD(above.x, above.y + CARD_BOTTOM, slot.x, slot.y + CARD_TOP)}
      />
      <rect
        x={slot.x - CARD_HALF_W + 2}
        y={slot.y + CARD_TOP}
        width={CARD_HALF_W * 2 - 4}
        height={CARD_BOTTOM - CARD_TOP}
        rx={12}
      />
    </g>
  );
}
