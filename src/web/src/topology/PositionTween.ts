import { notify } from "../core/state";

type Pt = { x: number; y: number };
type Tween = { from: Pt; to: Pt; start: number };

const DURATION_MS = 420;
const tweens = new Map<string, Tween>();
let raf = 0;

function ease(t: number): number {
  return 1 - Math.pow(1 - t, 3);
}

function sample(tw: Tween, now: number): { pos: Pt; done: boolean } {
  const t = Math.min(1, (now - tw.start) / DURATION_MS);
  const k = ease(t);
  return {
    pos: { x: tw.from.x + (tw.to.x - tw.from.x) * k, y: tw.from.y + (tw.to.y - tw.from.y) * k },
    done: t >= 1,
  };
}

function tick() {
  raf = 0;
  if (tweens.size === 0) return;
  notify();
  raf = requestAnimationFrame(tick);
}

function near(a: Pt, b: Pt): boolean {
  return Math.abs(a.x - b.x) < 0.5 && Math.abs(a.y - b.y) < 0.5;
}

/**
 * Position to draw for `id` heading to `target`. When the target jumps away
 * from what was drawn last frame, glide there instead of teleporting; a target
 * that keeps moving (mother being dragged) restarts from the current point, so
 * attached cards trail behind like a magnetic chain.
 */
export function tweenPos(id: string, target: Pt, last: Pt | undefined, instant: boolean): Pt {
  const now = performance.now();
  let tw = tweens.get(id);
  if (instant || !last) {
    tweens.delete(id);
    return target;
  }
  if (tw && !near(tw.to, target)) {
    tw = { from: sample(tw, now).pos, to: target, start: now };
    tweens.set(id, tw);
  } else if (!tw && !near(last, target)) {
    tw = { from: last, to: target, start: now };
    tweens.set(id, tw);
  }
  if (!tw) return target;
  const s = sample(tw, now);
  if (s.done) tweens.delete(id);
  if (!raf) raf = requestAnimationFrame(tick);
  return s.pos;
}

export function forgetTween(id: string) {
  tweens.delete(id);
}

/** True while any card is gliding — callers should skip expensive PCB routing. */
export function isTweening(): boolean {
  return tweens.size > 0;
}
