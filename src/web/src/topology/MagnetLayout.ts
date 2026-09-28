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

function dodgeColumnX(mother: Pt): number {
  const toRight = VIEW.cx + MASTER_EXCLUSION_R + CARD_HALF_W * 0.9;
  const toLeft = VIEW.cx - MASTER_EXCLUSION_R - CARD_HALF_W * 0.9;
  return Math.abs(toRight - mother.x) <= Math.abs(toLeft - mother.x) ? toRight : toLeft;
}

/** Shared X for mother + vertical stack; shifts left/right if any card would cover Master. */
export function stackColumnX(mother: Pt, slotCount: number): number {
  const x = mother.x;
  if (cardCenterOverlapsMaster(x, mother.y)) return dodgeColumnX(mother);
  for (let i = 0; i <= slotCount; i++) {
    const y = mother.y + STACK_PITCH * (i + 1);
    if (cardCenterOverlapsMaster(x, y)) return dodgeColumnX(mother);
  }
  return x;
}

/** Render / hit-test anchor for a mother (stored Y, dodged X). */
export function motherAnchor(mother: Pt, slotCount: number): Pt {
  return { x: stackColumnX(mother, slotCount), y: mother.y };
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
  let minX = colX;
  let maxX = colX;
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

/** Horizontal distance between side-by-side (clustered) cards. */
export const CLUSTER_PITCH = CARD_HALF_W * 2 + 22;

/** Where a card lands when it joins `anchor` on the given side. */
export function clusterSlot(anchor: Pt, side: -1 | 1): Pt {
  return { x: anchor.x + side * CLUSTER_PITCH, y: anchor.y };
}

/**
 * Free card of the same kind (mother / plain) that (x, y) sits right beside:
 * dropping there forms or joins a cluster. Cards stacked under a mother are
 * laid out by their stack and never act as cluster anchors.
 */
export function findClusterTarget(x: number, y: number, selfId: string): { id: string; side: -1 | 1 } | null {
  const self = state.placed.find((p) => p.node.id === selfId)?.node;
  if (!self) return null;
  let best: { id: string; side: -1 | 1 } | null = null;
  let bestErr = Infinity;
  for (const p of state.placed) {
    if (p.node.id === selfId || isMother(p.node) !== isMother(self)) continue;
    if (state.stacks.parentOf.has(p.node.id)) continue;
    const dy = Math.abs(y - p.y);
    const dx = x - p.x;
    if (dy > (CARD_BOTTOM - CARD_TOP) * 0.6) continue;
    if (Math.abs(dx) < CARD_HALF_W * 0.9 || Math.abs(dx) > CLUSTER_PITCH + CARD_HALF_W * 0.8) continue;
    const err = dy + Math.abs(Math.abs(dx) - CLUSTER_PITCH);
    if (err < bestErr) {
      bestErr = err;
      best = { id: p.node.id, side: dx < 0 ? -1 : 1 };
    }
  }
  return best;
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
    const colX = motherAnchor(pos, kids).x;
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
