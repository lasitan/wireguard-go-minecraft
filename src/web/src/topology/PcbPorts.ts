import { VIEW } from "../core/constants";
import type { PlacedNode } from "../core/models";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP } from "./MagnetLayout";
import { CLEAR, type Pt, type Rect } from "./PcbRouter";

function padRect(r: Rect, pad: number): Rect {
  return { minX: r.minX - pad, minY: r.minY - pad, maxX: r.maxX + pad, maxY: r.maxY + pad };
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

/** Axis-aligned card box for a placed agent. */
export function agentObstacle(p: PlacedNode, pad = CLEAR): Rect {
  return padRect(
    {
      minX: p.x - CARD_HALF_W,
      maxX: p.x + CARD_HALF_W,
      minY: p.y + CARD_TOP,
      maxY: p.y + CARD_BOTTOM,
    },
    pad,
  );
}

/** Master hub exclusion (square approx of the ring + clearance). */
export function masterObstacle(pad = CLEAR): Rect {
  const r = 42 + pad;
  return { minX: VIEW.cx - r, maxX: VIEW.cx + r, minY: VIEW.cy - r, maxY: VIEW.cy + r };
}

export type CardSide = "left" | "right" | "top" | "bottom";

/**
 * Attach point just outside a card edge facing `toward`. Sides in `blocked`
 * (taken by magnet / cluster joints) are skipped for the next best edge.
 */
export function portOnAgent(p: PlacedNode, towardX: number, towardY: number, blocked?: ReadonlySet<CardSide>): Pt {
  const cx = p.x;
  const cy = p.y + (CARD_TOP + CARD_BOTTOM) / 2;
  const left = p.x - CARD_HALF_W - CLEAR;
  const right = p.x + CARD_HALF_W + CLEAR;
  const top = p.y + CARD_TOP - CLEAR;
  const bottom = p.y + CARD_BOTTOM + CLEAR;
  const dx = towardX - cx;
  const dy = towardY - cy;
  const h: CardSide = dx >= 0 ? "right" : "left";
  const v: CardSide = dy >= 0 ? "bottom" : "top";
  const flip: Record<CardSide, CardSide> = { left: "right", right: "left", top: "bottom", bottom: "top" };
  const order: CardSide[] = Math.abs(dx) >= Math.abs(dy) ? [h, v, flip[v], flip[h]] : [v, h, flip[h], flip[v]];
  const side = order.find((s) => !blocked?.has(s)) ?? order[0];
  switch (side) {
    case "left":
      return { x: left, y: clamp(cy, top + 6, bottom - 6) };
    case "right":
      return { x: right, y: clamp(cy, top + 6, bottom - 6) };
    case "top":
      return { x: clamp(cx, left + 6, right - 6), y: top };
    default:
      return { x: clamp(cx, left + 6, right - 6), y: bottom };
  }
}

/** Attach point just outside the Master ring facing `toward`. */
export function portOnMaster(towardX: number, towardY: number): Pt {
  const dx = towardX - VIEW.cx;
  const dy = towardY - VIEW.cy;
  const len = Math.hypot(dx, dy) || 1;
  const r = 42 + CLEAR;
  return { x: VIEW.cx + (dx / len) * r, y: VIEW.cy + (dy / len) * r };
}

/** Build obstacle map for every placed agent. */
export function buildAgentObstacles(placed: PlacedNode[]): Map<string, Rect> {
  const m = new Map<string, Rect>();
  for (const p of placed) m.set(p.node.id, agentObstacle(p));
  return m;
}
