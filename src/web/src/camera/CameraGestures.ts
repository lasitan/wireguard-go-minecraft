import { ZOOM_MAX_W, ZOOM_MIN_W } from "../core/constants";
import type { Cam } from "../core/models";
import { applyCamera, readSvgCamera, stopCameraTween } from "./CameraController";
import { camWithAspect, homeCam, setViewportAspect, viewportAspect } from "./CameraMath";
import { clientToSvg } from "./SvgCoords";

export function clampZoomWidth(w: number): number {
  return Math.min(ZOOM_MAX_W, Math.max(ZOOM_MIN_W, w));
}

/** Pan camera by a screen-pixel delta (pointer drag). */
export function panCameraByScreenDelta(
  svg: SVGSVGElement,
  fromClientX: number,
  fromClientY: number,
  toClientX: number,
  toClientY: number,
) {
  stopCameraTween();
  const cam = readSvgCamera();
  const a = clientToSvg(svg, fromClientX, fromClientY);
  const b = clientToSvg(svg, toClientX, toClientY);
  applyCamera({
    x: cam.x - (b.x - a.x),
    y: cam.y - (b.y - a.y),
    w: cam.w,
    h: cam.h,
  });
}

/** Zoom camera toward the cursor (wheel). factor < 1 zooms in. */
export function zoomCameraAtClient(svg: SVGSVGElement, clientX: number, clientY: number, factor: number) {
  stopCameraTween();
  const cam = readSvgCamera();
  const focus = clientToSvg(svg, clientX, clientY);
  const nextW = clampZoomWidth(cam.w * factor);
  const ratio = nextW / cam.w;
  const nextH = nextW / viewportAspect();
  // Keep the point under cursor stable.
  applyCamera({
    x: focus.x - (focus.x - cam.x) * ratio,
    y: focus.y - (focus.y - cam.y) * (nextH / cam.h),
    w: nextW,
    h: nextH,
  });
}

/** Match viewBox aspect to the SVG element's CSS box (call on mount / resize). */
export function syncViewportAspect(svg: SVGSVGElement) {
  const aw = svg.clientWidth;
  const ah = svg.clientHeight;
  if (aw < 2 || ah < 2) return;
  const next = aw / ah;
  if (Math.abs(next - viewportAspect()) < 0.001) return;
  setViewportAspect(next);
  applyCamera(camWithAspect(readSvgCamera(), next));
}

export function camAspectHome(): Cam {
  return homeCam();
}
