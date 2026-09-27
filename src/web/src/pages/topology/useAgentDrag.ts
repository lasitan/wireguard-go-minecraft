import { useRef, type PointerEvent as ReactPointerEvent } from "react";
import { DRAG_CLICK_PX } from "../../core/constants";
import { clientToSvg } from "../../camera/SvgCoords";
import { notify, state } from "../../core/state";
import { setNodePosition } from "../../topology/SyncPlacedNodes";
import { findMagnetTarget } from "../../topology/MagnetLayout";
import { isMother } from "../../topology/MagnetStacks";
import { attachNode } from "../../app/NodeActions";
import { focusTarget } from "../../app/FocusNav";

type DragState = {
  id: string;
  pointerId: number;
  startClientX: number;
  startClientY: number;
  grabDx: number;
  grabDy: number;
  moved: boolean;
  /** Mother the card was attached to when the drag began. */
  parentAtStart: string | null;
  canSnap: boolean;
};

/**
 * Pointer drag for agents. Short press → focus; drag → reposition. Plain
 * cards snap under a mother card on release (attach / switch), and dragging
 * an attached card out of its stack detaches it.
 */
export function useAgentDrag() {
  const drag = useRef<DragState | null>(null);

  function onPointerDown(e: ReactPointerEvent<SVGGElement>, id: string, x: number, y: number) {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    const node = state.mesh?.nodes.find((n) => n.id === id);
    drag.current = {
      id,
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      grabDx: pt.x - x,
      grabDy: pt.y - y,
      moved: false,
      parentAtStart: state.stacks.parentOf.get(id) || null,
      canSnap: !isMother(node),
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    const dist = Math.hypot(e.clientX - d.startClientX, e.clientY - d.startClientY);
    if (!d.moved && dist >= DRAG_CLICK_PX) {
      d.moved = true;
      state.draggingId = d.id;
      e.currentTarget.classList.add("is-dragging");
    }
    if (!d.moved) return;

    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    const x = pt.x - d.grabDx;
    const y = pt.y - d.grabDy;
    setNodePosition(d.id, x, y);
    if (d.canSnap) state.magnetTarget = findMagnetTarget(x, y, d.id);
    notify();
  }

  function onPointerUp(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    drag.current = null;
    e.currentTarget.classList.remove("is-dragging");
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
    if (!d.moved) {
      void focusTarget(d.id);
      return;
    }
    const target = d.canSnap ? state.magnetTarget : null;
    state.draggingId = null;
    state.magnetTarget = null;
    notify();
    if (target === d.parentAtStart || (!target && !d.parentAtStart)) return;
    attachNode(d.id, target || "").catch((err: Error) => {
      state.err = err.message;
      notify();
    });
  }

  return { onPointerDown, onPointerMove, onPointerUp };
}
