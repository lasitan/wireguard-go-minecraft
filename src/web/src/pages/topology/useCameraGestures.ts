import { useEffect, type RefObject } from "react";
import { dragClickPx } from "../../core/constants";
import { panCameraByScreenDelta, syncViewportAspect, zoomCameraAtClient } from "../../camera/CameraGestures";
import { stopCameraTween } from "../../camera/CameraController";
import { goHome } from "../../app/FocusNav";

type PanSession = {
  pointerId: number;
  lastX: number;
  lastY: number;
  startX: number;
  startY: number;
  moved: boolean;
  threshold: number;
};

type Pinch = { dist: number; midX: number; midY: number };

/**
 * Background pan (drag empty canvas), wheel zoom and two-finger pinch zoom on
 * the mesh SVG. Agent/hub pointer events stopPropagation so they don't start a pan.
 */
export function useCameraGestures(svgRef: RefObject<SVGSVGElement | null>) {
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;

    let pan: PanSession | null = null;
    /** Background touches currently down, for pinch. */
    const touches = new Map<number, { x: number; y: number }>();
    let pinch: Pinch | null = null;
    /** A pinch happened in this gesture: lifting fingers must not count as a tap. */
    let pinched = false;

    const syncAspect = () => syncViewportAspect(svg);
    syncAspect();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(syncAspect) : null;
    ro?.observe(svg);
    window.addEventListener("resize", syncAspect);

    const pinchState = (): Pinch | null => {
      if (touches.size < 2) return null;
      const [a, b] = [...touches.values()];
      return { dist: Math.hypot(b.x - a.x, b.y - a.y), midX: (a.x + b.x) / 2, midY: (a.y + b.y) / 2 };
    };

    const startPan = (e: PointerEvent) => {
      pan = {
        pointerId: e.pointerId,
        lastX: e.clientX,
        lastY: e.clientY,
        startX: e.clientX,
        startY: e.clientY,
        moved: pinched,
        threshold: dragClickPx(e.pointerType),
      };
    };

    const onPointerDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      const t = e.target as Element | null;
      if (t?.closest?.(".node-wrap, .hub, .settings-drawer, .edge-legend")) return;
      stopCameraTween();
      svg.setPointerCapture(e.pointerId);
      svg.classList.add("is-panning");
      if (e.pointerType === "touch") {
        touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
        if (touches.size >= 2) {
          pan = null;
          pinched = true;
          pinch = pinchState();
          return;
        }
      }
      startPan(e);
    };

    const onPointerMove = (e: PointerEvent) => {
      if (touches.has(e.pointerId)) touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pinch) {
        const next = pinchState();
        if (!next) return;
        panCameraByScreenDelta(svg, pinch.midX, pinch.midY, next.midX, next.midY);
        if (next.dist > 1 && pinch.dist > 1) zoomCameraAtClient(svg, next.midX, next.midY, pinch.dist / next.dist);
        pinch = next;
        return;
      }
      if (!pan || e.pointerId !== pan.pointerId) return;
      if (!pan.moved) {
        const total = Math.hypot(e.clientX - pan.startX, e.clientY - pan.startY);
        if (total >= pan.threshold) pan.moved = true;
      }
      if (pan.moved) {
        panCameraByScreenDelta(svg, pan.lastX, pan.lastY, e.clientX, e.clientY);
        pan.lastX = e.clientX;
        pan.lastY = e.clientY;
      }
    };

    const endPan = (e: PointerEvent) => {
      try {
        svg.releasePointerCapture(e.pointerId);
      } catch {
        /* already released */
      }
      if (touches.delete(e.pointerId) && pinch) {
        pinch = pinchState();
        if (!pinch) {
          // One finger left: keep panning with it.
          const [id, p] = [...touches.entries()][0] || [];
          if (id !== undefined && p) {
            pan = { pointerId: id, lastX: p.x, lastY: p.y, startX: p.x, startY: p.y, moved: true, threshold: 0 };
          }
        }
        if (touches.size === 0) svg.classList.remove("is-panning");
        return;
      }
      if (!pan || e.pointerId !== pan.pointerId) return;
      const wasClick = !pan.moved;
      pan = null;
      if (touches.size === 0) {
        pinched = false;
        svg.classList.remove("is-panning");
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
      ro?.disconnect();
      window.removeEventListener("resize", syncAspect);
      svg.removeEventListener("pointerdown", onPointerDown);
      svg.removeEventListener("pointermove", onPointerMove);
      svg.removeEventListener("pointerup", endPan);
      svg.removeEventListener("pointercancel", endPan);
      svg.removeEventListener("wheel", onWheel);
    };
  }, [svgRef]);
}
