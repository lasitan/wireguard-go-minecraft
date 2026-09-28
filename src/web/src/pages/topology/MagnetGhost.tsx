import { state } from "../../core/state";
import {
  CARD_BOTTOM,
  CARD_HALF_W,
  CARD_TOP,
  clusterSlot,
  motherAnchor,
  slotPos,
  stackChildren,
} from "../../topology/MagnetLayout";
import { pcbStackRouteD } from "../../topology/PcbRoute";

function SlotRect({ x, y }: { x: number; y: number }) {
  return (
    <rect
      x={x - CARD_HALF_W + 2}
      y={y + CARD_TOP}
      width={CARD_HALF_W * 2 - 4}
      height={CARD_BOTTOM - CARD_TOP}
      rx={12}
    />
  );
}

/** Dashed slot where the dragged card will land: under a mother, or beside a cluster card. */
export function MagnetGhost() {
  const dragging = state.draggingId;
  if (!dragging) return null;

  const side = state.clusterTarget;
  const anchor = side ? state.placed.find((p) => p.node.id === side.id) : undefined;
  if (side && anchor) {
    const slot = clusterSlot(anchor, side.side);
    const midY = (CARD_TOP + CARD_BOTTOM) / 2;
    const x1 = anchor.x + side.side * CARD_HALF_W;
    const x2 = slot.x - side.side * CARD_HALF_W;
    return (
      <g key={`cluster:${side.id}:${side.side}`} className="magnet-ghost">
        <path className="magnet-ghost-rail" d={`M ${x1} ${anchor.y + midY} L ${x2} ${slot.y + midY}`} />
        <SlotRect x={slot.x} y={slot.y} />
      </g>
    );
  }

  const target = state.magnetTarget;
  const mother = target ? state.nodePositions[target] : undefined;
  if (!target || !mother) return null;
  const n = stackChildren(target, dragging).length;
  const slot = slotPos(mother, n, n);
  const above = n === 0 ? motherAnchor(mother, n) : slotPos(mother, n - 1, n);
  return (
    <g key={target} className="magnet-ghost">
      <path
        className="magnet-ghost-rail"
        d={pcbStackRouteD(above.x, above.y + CARD_BOTTOM, slot.x, slot.y + CARD_TOP)}
      />
      <SlotRect x={slot.x} y={slot.y} />
    </g>
  );
}
