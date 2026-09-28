import { VIEW } from "../core/constants";
import { state } from "../core/state";
import { isMother } from "./MagnetStacks";

/** Card box relative to its anchor (see AgentNodes foreignObject + .node-card height). */
export const CARD_TOP = -34;
export const CARD_BOTTOM = 24;
export const CARD_HALF_W = 78;
/** Vertical distance between stacked cards: card height + rail gap. */
export const STACK_PITCH = 70;

/** Master hub + label — stacked cards dodge sideways when a slot would overlap. */
export const MASTER_EXCLUSION_R = 58;

type Pt = { x: number; y: number };

function cardCenterOverlapsMaster(anchorX: number, anchorY: number): boolean {
  const cy = anchorY + (CARD_TOP + CARD_BOTTOM) / 2;
  return Math.hypot(anchorX - VIEW.cx, cy - VIEW.cy) < MASTER_EXCLUSION_R + 18;
}

/** Shared X for a vertical stack column; shifts left/right if slots would cover Master. */
export function stackColumnX(mother: Pt, slotCount: number): number {
  let x = mother.x;
  for (let i = 0; i <= slotCount; i++) {
    const y = mother.y + STACK_PITCH * (i + 1);
    if (!cardCenterOverlapsMaster(x, y)) continue;
    const toRight = VIEW.cx + MASTER_EXCLUSION_R + CARD_HALF_W * 0.9;
    const toLeft = VIEW.cx - MASTER_EXCLUSION_R - CARD_HALF_W * 0.9;
    return Math.abs(toRight - mother.x) <= Math.abs(toLeft - mother.x) ? toRight : toLeft;
  }
  return x;
}

/** Child slot index (0-based); always stacks downward. slotCount sizes the column dodge. */
export function slotPos(mother: Pt, index: number, slotCount = index): Pt {
  const x = stackColumnX(mother, slotCount);
  return { x, y: mother.y + STACK_PITCH * (index + 1) };
}

/** Axis-aligned bounds of a mother column including all stack slots. */
export function stackHitBounds(mother: Pt, slotCount: number) {
  const colX = stackColumnX(mother, slotCount);
  const padX = CARD_HALF_W * 1.4;
  let minX = Math.min(mother.x, colX);
  let maxX = Math.max(mother.x, colX);
  let minY = mother.y + CARD_TOP - 12;
  let maxY = mother.y + CARD_BOTTOM + 24;
  for (let i = 0; i <= slotCount; i++) {
    const y = mother.y + STACK_PITCH * (i + 1);
    minY = Math.min(minY, y + CARD_TOP - 12);
    maxY = Math.max(maxY, y + CARD_BOTTOM + 24);
  }
  return { minX: minX - padX, maxX: maxX + padX, minY, maxY };
}

/** Children of a mother, minus the card being dragged out of the stack. */
export function stackChildren(motherId: string, exclude?: string | null): string[] {
  const kids = state.stacks.childrenOf.get(motherId) || [];
  return exclude ? kids.filter((k) => k !== exclude) : kids;
}

/**
 * Mother whose stack column contains (x, y): vertically from the mother's top
 * down to one free slot below the stack; horizontally the mother + dodged column.
 */
export function findMagnetTarget(x: number, y: number, selfId: string): string | null {
  let best: string | null = null;
  let bestDx = Infinity;
  for (const p of state.placed) {
    if (p.node.id === selfId || !isMother(p.node)) continue;
    const pos = state.nodePositions[p.node.id] || p;
    const kids = stackChildren(p.node.id, selfId).length;
    const colX = stackColumnX(pos, kids);
    const b = stackHitBounds(pos, kids);
    if (x < b.minX || x > b.maxX || y < b.minY || y > b.maxY) continue;
    const dx = Math.abs(x - colX);
    if (dx < bestDx) {
      best = p.node.id;
      bestDx = dx;
    }
  }
  return best;
}
