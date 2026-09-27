import { useRef, type PointerEvent as ReactPointerEvent } from "react";
import { DRAG_CLICK_PX } from "../../core/constants";
import { clientToSvg } from "../../camera/SvgCoords";
import { notify } from "../../core/state";
import { setNodePosition } from "../../topology/SyncPlacedNodes";
import { focusTarget } from "../../app/FocusNav";

type DragState = {
  id: string;
  pointerId: number;
  startClientX: number;
  startClientY: number;
  originX: number;
  originY: number;
  grabDx: number;
  grabDy: number;
  moved: boolean;
};

/**
 * Pointer drag for agents. Short press → focus; drag → reposition + rebuild edges.
 */
export function useAgentDrag() {
  const drag = useRef<DragState | null>(null);

  function onPointerDown(
    e: ReactPointerEvent<SVGGElement>,
    id: string,
    x: number,
    y: number,
  ) {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    drag.current = {
      id,
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      originX: x,
      originY: y,
      grabDx: pt.x - x,
      grabDy: pt.y - y,
      moved: false,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
    e.currentTarget.classList.add("is-dragging");
  }

  function onPointerMove(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    const dist = Math.hypot(e.clientX - d.startClientX, e.clientY - d.startClientY);
    if (!d.moved && dist >= DRAG_CLICK_PX) d.moved = true;
    if (!d.moved) return;

    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    setNodePosition(d.id, pt.x - d.grabDx, pt.y - d.grabDy);
    notify();
  }

  function onPointerUp(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    const moved = d.moved;
    const id = d.id;
    drag.current = null;
    e.currentTarget.classList.remove("is-dragging");
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
    if (!moved) void focusTarget(id);
    else notify();
  }

  return { onPointerDown, onPointerMove, onPointerUp };
}
