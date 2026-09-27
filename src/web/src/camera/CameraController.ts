import { CAM_MS } from "../core/constants";
import { state } from "../core/state";
import type { Cam } from "../core/models";
import { camsNear, camToAttr, easeInOutCubic } from "./CameraMath";

let cameraRaf = 0;
let cameraDone: (() => void) | null = null;

export function isCameraAnimating(): boolean {
  return cameraRaf !== 0;
}

export function readSvgCamera(): Cam {
  const svg = document.querySelector(".mesh-svg") as SVGSVGElement | null;
  if (!svg) return { ...state.camera };
  const raw = svg.getAttribute("viewBox");
  if (!raw) return { ...state.camera };
  const [x, y, w, h] = raw.split(/\s+/).map(Number);
  if ([x, y, w, h].some((n) => Number.isNaN(n))) return { ...state.camera };
  return { x, y, w, h };
}

export function applyCamera(c: Cam) {
  state.camera = c;
  const svg = document.querySelector(".mesh-svg") as SVGSVGElement | null;
  if (svg) svg.setAttribute("viewBox", camToAttr(c));
}

export function stopCameraTween() {
  if (cameraRaf) {
    cancelAnimationFrame(cameraRaf);
    cameraRaf = 0;
  }
  if (cameraDone) {
    const done = cameraDone;
    cameraDone = null;
    done();
  }
}

/** Smoothly fly camera from the current on-screen position to target. */
export function animateCameraTo(target: Cam, ms = CAM_MS): Promise<void> {
  return new Promise((resolve) => {
    stopCameraTween();
    const from = readSvgCamera();
    if (camsNear(from, target)) {
      applyCamera(target);
      resolve();
      return;
    }
    cameraDone = resolve;
    const t0 = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - t0) / ms);
      const e = easeInOutCubic(p);
      applyCamera({
        x: from.x + (target.x - from.x) * e,
        y: from.y + (target.y - from.y) * e,
        w: from.w + (target.w - from.w) * e,
        h: from.h + (target.h - from.h) * e,
      });
      if (p < 1) {
        cameraRaf = requestAnimationFrame(tick);
      } else {
        cameraRaf = 0;
        const done = cameraDone;
        cameraDone = null;
        done?.();
      }
    };
    cameraRaf = requestAnimationFrame(tick);
  });
}
