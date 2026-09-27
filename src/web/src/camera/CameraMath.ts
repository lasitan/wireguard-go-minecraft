import { FOCUS_SCALE, VIEW } from "../core/constants";
import type { Cam, Focus } from "../core/models";

export function homeCam(): Cam {
  return { x: 0, y: 0, w: VIEW.w, h: VIEW.h };
}

export function focusToCam(f: Focus): Cam {
  const w = VIEW.w / f.scale;
  const h = VIEW.h / f.scale;
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
