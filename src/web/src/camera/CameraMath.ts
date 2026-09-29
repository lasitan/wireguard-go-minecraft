import { FOCUS_SCALE, VIEW } from "../core/constants";
import type { Cam, Focus } from "../core/models";

/** Live SVG element aspect (width/height). Keeps viewBox matched to the screen
 * so letterboxing does not shift perceived card layout across monitors. */
let aspect = VIEW.w / VIEW.h;

export function viewportAspect(): number {
  return aspect;
}

export function setViewportAspect(a: number) {
  if (!(a > 0.15 && a < 8)) return;
  aspect = a;
}

export function homeCam(): Cam {
  const w = VIEW.w;
  const h = w / aspect;
  return { x: VIEW.cx - w / 2, y: VIEW.cy - h / 2, w, h };
}

export function focusToCam(f: Focus): Cam {
  const w = VIEW.w / f.scale;
  const h = w / aspect;
  return { x: f.x - w / 2, y: f.y - h / 2, w, h };
}

export function camToAttr(c: Cam): string {
  return `${c.x} ${c.y} ${c.w} ${c.h}`;
}

export function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

export function camsNear(a: Cam, b: Cam, eps = 0.5): boolean {
  return (
    Math.abs(a.x - b.x) < eps &&
    Math.abs(a.y - b.y) < eps &&
    Math.abs(a.w - b.w) < eps &&
    Math.abs(a.h - b.h) < eps
  );
}

export function focusCamAt(x: number, y: number): Cam {
  return focusToCam({ x, y, scale: FOCUS_SCALE });
}

/** Keep the current center + zoom width; rewrite height for a new aspect. */
export function camWithAspect(cam: Cam, nextAspect: number): Cam {
  const cx = cam.x + cam.w / 2;
  const cy = cam.y + cam.h / 2;
  const w = cam.w;
  const h = w / nextAspect;
  return { x: cx - w / 2, y: cy - h / 2, w, h };
}
