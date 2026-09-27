import { useEffect, type RefObject } from "react";
import { DRAG_CLICK_PX } from "../../core/constants";
import { panCameraByScreenDelta, zoomCameraAtClient } from "../../camera/CameraGestures";
import { stopCameraTween } from "../../camera/CameraController";
import { goHome } from "../../app/FocusNav";

type PanSession = {
  pointerId: number;
  lastX: number;
  lastY: number;
  startX: number;
  startY: number;
  moved: boolean;
};

/**
 * Background pan (drag empty canvas) + wheel zoom on the mesh SVG.
 * Agent/hub pointer events stopPropagation so they don't start a pan.
 */
export function useCameraGestures(svgRef: RefObject<SVGSVGElement | null>) {
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;

    let pan: PanSession | null = null;

    const onPointerDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      const t = e.target as Element | null;
      if (t?.closest?.(".node-wrap, .hub, .settings-drawer, .edge-legend")) return;
      stopCameraTween();
      pan = {
        pointerId: e.pointerId,
        lastX: e.clientX,
        lastY: e.clientY,
        startX: e.clientX,
        startY: e.clientY,
        moved: false,
      };
      svg.setPointerCapture(e.pointerId);
      svg.classList.add("is-panning");
    };

    const onPointerMove = (e: PointerEvent) => {
      if (!pan || e.pointerId !== pan.pointerId) return;
      if (!pan.moved) {
        const total = Math.hypot(e.clientX - pan.startX, e.clientY - pan.startY);
        if (total >= DRAG_CLICK_PX) pan.moved = true;
      }
      if (pan.moved) {
        panCameraByScreenDelta(svg, pan.lastX, pan.lastY, e.clientX, e.clientY);
        pan.lastX = e.clientX;
        pan.lastY = e.clientY;
      }
    };

    const endPan = (e: PointerEvent) => {
      if (!pan || e.pointerId !== pan.pointerId) return;
      const wasClick = !pan.moved;
      pan = null;
      svg.classList.remove("is-panning");
      try {
        svg.releasePointerCapture(e.pointerId);
      } catch {
        /* already released */
      }
      if (wasClick) void goHome();
    };

    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const factor = e.deltaY > 0 ? 1.08 : 1 / 1.08;
      zoomCameraAtClient(svg, e.clientX, e.clientY, factor);
    };

    svg.addEventListener("pointerdown", onPointerDown);
    svg.addEventListener("pointermove", onPointerMove);
    svg.addEventListener("pointerup", endPan);
    svg.addEventListener("pointercancel", endPan);
    svg.addEventListener("wheel", onWheel, { passive: false });

    return () => {
      svg.removeEventListener("pointerdown", onPointerDown);
      svg.removeEventListener("pointermove", onPointerMove);
      svg.removeEventListener("pointerup", endPan);
      svg.removeEventListener("pointercancel", endPan);
      svg.removeEventListener("wheel", onWheel);
    };
  }, [svgRef]);
}
