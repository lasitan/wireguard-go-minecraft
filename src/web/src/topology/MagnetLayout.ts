import { state } from "../core/state";
import { isMother } from "./MagnetStacks";

/** Card box relative to its anchor (see AgentNodes foreignObject + .node-card height). */
export const CARD_TOP = -34;
export const CARD_BOTTOM = 24;
export const CARD_HALF_W = 78;
/** Vertical distance between stacked cards: card height + rail gap. */
export const STACK_PITCH = 70;

type Pt = { x: number; y: number };

export function slotPos(mother: Pt, index: number): Pt {
  return { x: mother.x, y: mother.y + STACK_PITCH * (index + 1) };
}

/** Children of a mother, minus the card being dragged out of the stack. */
export function stackChildren(motherId: string, exclude?: string | null): string[] {
  const kids = state.stacks.childrenOf.get(motherId) || [];
  return exclude ? kids.filter((k) => k !== exclude) : kids;
}

/**
 * Mother whose stack column contains (x, y): horizontally within one card
 * width, vertically from the mother's top down to one free slot below the stack.
 */
export function findMagnetTarget(x: number, y: number, selfId: string): string | null {
  let best: string | null = null;
  let bestDx = Infinity;
  for (const p of state.placed) {
    if (p.node.id === selfId || !isMother(p.node)) continue;
    const pos = state.nodePositions[p.node.id] || p;
    const dx = Math.abs(x - pos.x);
    if (dx > CARD_HALF_W * 1.4) continue;
    const kids = stackChildren(p.node.id, selfId).length;
    const top = pos.y + CARD_TOP - 12;
    const bottom = slotPos(pos, kids).y + CARD_BOTTOM + 24;
    if (y < top || y > bottom) continue;
    if (dx < bestDx) {
      best = p.node.id;
      bestDx = dx;
    }
  }
  return best;
}
