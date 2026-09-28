import { MASTER_ID, VIEW } from "../core/constants";
import type { PlacedNode } from "../core/models";
import { CARD_BOTTOM, CARD_HALF_W, CARD_TOP } from "./MagnetLayout";
import { pathDFromPoints } from "./PcbRoute";

export type Pt = { x: number; y: number };

export type Rect = { minX: number; minY: number; maxX: number; maxY: number };

export type RouteRequest = {
  id: string;
  fromId: string;
  toId: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
};

const CELL = 12;
const CLEAR = 8;
const TURN_COST = 0.4;
const MAX_EXPAND = 100_000;

function padRect(r: Rect, pad: number): Rect {
  return { minX: r.minX - pad, minY: r.minY - pad, maxX: r.maxX + pad, maxY: r.maxY + pad };
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
  const r = 42 + pad; // hub-ring radius + clearance
  return { minX: VIEW.cx - r, maxX: VIEW.cx + r, minY: VIEW.cy - r, maxY: VIEW.cy + r };
}

/** Attach point just outside a card edge facing `toward`. */
export function portOnAgent(p: PlacedNode, towardX: number, towardY: number): Pt {
  const cx = p.x;
  const cy = p.y + (CARD_TOP + CARD_BOTTOM) / 2;
  const left = p.x - CARD_HALF_W - CLEAR;
  const right = p.x + CARD_HALF_W + CLEAR;
  const top = p.y + CARD_TOP - CLEAR;
  const bottom = p.y + CARD_BOTTOM + CLEAR;
  const dx = towardX - cx;
  const dy = towardY - cy;
  if (Math.abs(dx) >= Math.abs(dy)) {
    return { x: dx >= 0 ? right : left, y: clamp(cy, top + 6, bottom - 6) };
  }
  return { x: clamp(cx, left + 6, right - 6), y: dy >= 0 ? bottom : top };
}

/** Attach point just outside the Master ring facing `toward`. */
export function portOnMaster(towardX: number, towardY: number): Pt {
  const dx = towardX - VIEW.cx;
  const dy = towardY - VIEW.cy;
  const len = Math.hypot(dx, dy) || 1;
  const r = 42 + CLEAR;
  return { x: VIEW.cx + (dx / len) * r, y: VIEW.cy + (dy / len) * r };
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

function rectsOverlap(a: Rect, b: Rect): boolean {
  return !(a.maxX < b.minX || a.minX > b.maxX || a.maxY < b.minY || a.minY > b.maxY);
}

function pointInRect(x: number, y: number, r: Rect): boolean {
  return x >= r.minX && x <= r.maxX && y >= r.minY && y <= r.maxY;
}

function segmentHitsRect(ax: number, ay: number, bx: number, by: number, r: Rect): boolean {
  const minX = Math.min(ax, bx);
  const maxX = Math.max(ax, bx);
  const minY = Math.min(ay, by);
  const maxY = Math.max(ay, by);
  return rectsOverlap({ minX, minY, maxX, maxY }, r);
}

function simplifyOrthogonal(pts: Pt[]): Pt[] {
  if (pts.length <= 2) return pts;
  const out: Pt[] = [pts[0]];
  for (let i = 1; i < pts.length - 1; i++) {
    const a = out[out.length - 1];
    const b = pts[i];
    const c = pts[i + 1];
    const colinear =
      (Math.abs(a.x - b.x) < 0.01 && Math.abs(b.x - c.x) < 0.01) ||
      (Math.abs(a.y - b.y) < 0.01 && Math.abs(b.y - c.y) < 0.01);
    if (!colinear) out.push(b);
  }
  out.push(pts[pts.length - 1]);
  return out;
}

function ensureOrthogonalEnds(pts: Pt[], x1: number, y1: number, x2: number, y2: number): Pt[] {
  if (pts.length === 0) return [{ x: x1, y: y1 }, { x: x2, y: y2 }];
  const out = pts.map((p) => ({ ...p }));
  out[0] = { x: x1, y: y1 };
  out[out.length - 1] = { x: x2, y: y2 };
  if (out.length >= 2) {
    const a = out[0];
    const b = out[1];
    if (Math.abs(a.x - b.x) > 0.01 && Math.abs(a.y - b.y) > 0.01) {
      out.splice(1, 0, { x: b.x, y: a.y });
    }
  }
  if (out.length >= 2) {
    const a = out[out.length - 2];
    const b = out[out.length - 1];
    if (Math.abs(a.x - b.x) > 0.01 && Math.abs(a.y - b.y) > 0.01) {
      out.splice(out.length - 1, 0, { x: b.x, y: a.y });
    }
  }
  return simplifyOrthogonal(out);
}

type Cell = { i: number; j: number };

function cellKey(i: number, j: number): number {
  return ((i + 4096) << 13) | (j + 4096);
}

/**
 * Bundle-route orthogonal PCB traces: avoid agent/Master obstacles and do not
 * cross previously placed traces (single-layer). Falls back to an L-bend when
 * A* cannot find a path.
 */
export function routePcbBundle(
  requests: RouteRequest[],
  obstaclesById: Map<string, Rect>,
  masterBlocked: Rect,
): Map<string, string> {
  const out = new Map<string, string>();
  if (requests.length === 0) return out;

  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  const grow = (x: number, y: number) => {
    minX = Math.min(minX, x);
    minY = Math.min(minY, y);
    maxX = Math.max(maxX, x);
    maxY = Math.max(maxY, y);
  };
  for (const r of obstaclesById.values()) {
    grow(r.minX, r.minY);
    grow(r.maxX, r.maxY);
  }
  grow(masterBlocked.minX, masterBlocked.minY);
  grow(masterBlocked.maxX, masterBlocked.maxY);
  for (const req of requests) {
    grow(req.x1, req.y1);
    grow(req.x2, req.y2);
  }
  const margin = CELL * 10;
  minX -= margin;
  minY -= margin;
  maxX += margin;
  maxY += margin;

  const toCell = (x: number, y: number): Cell => ({
    i: Math.round((x - minX) / CELL),
    j: Math.round((y - minY) / CELL),
  });
  const toWorld = (c: Cell): Pt => ({ x: minX + c.i * CELL, y: minY + c.j * CELL });

  const occupied = new Set<number>();

  const markPathCells = (pts: Pt[]) => {
    for (let s = 1; s < pts.length; s++) {
      const a = pts[s - 1];
      const b = pts[s];
      if (Math.abs(a.x - b.x) < 0.01) {
        const x = a.x;
        const y0 = Math.min(a.y, b.y);
        const y1 = Math.max(a.y, b.y);
        for (let y = y0; y <= y1 + CELL * 0.5; y += CELL) {
          const c = toCell(x, y);
          occupied.add(cellKey(c.i, c.j));
        }
      } else {
        const y = a.y;
        const x0 = Math.min(a.x, b.x);
        const x1 = Math.max(a.x, b.x);
        for (let x = x0; x <= x1 + CELL * 0.5; x += CELL) {
          const c = toCell(x, y);
          occupied.add(cellKey(c.i, c.j));
        }
      }
    }
    // Free endpoints so other traces can still attach to the same widget rim.
    if (pts.length) {
      const a = toCell(pts[0].x, pts[0].y);
      const b = toCell(pts[pts.length - 1].x, pts[pts.length - 1].y);
      occupied.delete(cellKey(a.i, a.j));
      occupied.delete(cellKey(b.i, b.j));
    }
  };

  const hardObstacles = (): Rect[] => {
    const hard: Rect[] = [masterBlocked];
    for (const r of obstaclesById.values()) hard.push(r);
    return hard;
  };

  const fallbackL = (req: RouteRequest): Pt[] => {
    const hard = hardObstacles();
    const viaH: Pt[] = [
      { x: req.x1, y: req.y1 },
      { x: req.x2, y: req.y1 },
      { x: req.x2, y: req.y2 },
    ];
    const viaV: Pt[] = [
      { x: req.x1, y: req.y1 },
      { x: req.x1, y: req.y2 },
      { x: req.x2, y: req.y2 },
    ];
    const score = (pts: Pt[]) => {
      let hits = 0;
      for (let i = 1; i < pts.length; i++) {
        for (const r of hard) {
          if (segmentHitsRect(pts[i - 1].x, pts[i - 1].y, pts[i].x, pts[i].y, r)) hits++;
        }
      }
      return hits;
    };
    return score(viaH) <= score(viaV) ? viaH : viaV;
  };

  const astar = (req: RouteRequest): Pt[] | null => {
    const hard = hardObstacles();
    const start = toCell(req.x1, req.y1);
    const goal = toCell(req.x2, req.y2);

    const isBlocked = (i: number, j: number): boolean => {
      // Ports sit on the padded obstacle rim — always allow endpoints.
      if ((i === start.i && j === start.j) || (i === goal.i && j === goal.j)) return false;
      if (occupied.has(cellKey(i, j))) return true;
      const p = toWorld({ i, j });
      for (const r of hard) {
        if (pointInRect(p.x, p.y, r)) return true;
      }
      return false;
    };

    type Node = { i: number; j: number; g: number; f: number; dir: number };
    const open: Node[] = [];
    const gScore = new Map<number, number>();
    const parent = new Map<number, number>(); // childKey -> parentKey
    const h = (i: number, j: number) => Math.abs(i - goal.i) + Math.abs(j - goal.j);
    const dirs = [
      { di: 1, dj: 0, d: 1 },
      { di: -1, dj: 0, d: 2 },
      { di: 0, dj: 1, d: 3 },
      { di: 0, dj: -1, d: 4 },
    ];

    const sk = cellKey(start.i, start.j);
    gScore.set(sk, 0);
    open.push({ i: start.i, j: start.j, g: 0, f: h(start.i, start.j), dir: 0 });

    let expands = 0;
    while (open.length && expands++ < MAX_EXPAND) {
      let best = 0;
      for (let k = 1; k < open.length; k++) if (open[k].f < open[best].f) best = k;
      const cur = open[best];
      open[best] = open[open.length - 1];
      open.pop();

      if (cur.i === goal.i && cur.j === goal.j) {
        const cells: Cell[] = [{ i: cur.i, j: cur.j }];
        let ck = cellKey(cur.i, cur.j);
        while (parent.has(ck)) {
          const pk = parent.get(ck)!;
          const pi = (pk >> 13) - 4096;
          const pj = (pk & 8191) - 4096;
          cells.push({ i: pi, j: pj });
          ck = pk;
          if (pi === start.i && pj === start.j) break;
        }
        cells.reverse();
        const world = cells.map(toWorld);
        return ensureOrthogonalEnds(world, req.x1, req.y1, req.x2, req.y2);
      }

      const ck = cellKey(cur.i, cur.j);
      const known = gScore.get(ck);
      if (known !== undefined && cur.g > known + 1e-6) continue;

      for (const { di, dj, d } of dirs) {
        const ni = cur.i + di;
        const nj = cur.j + dj;
        if (isBlocked(ni, nj)) continue;
        const turn = cur.dir !== 0 && cur.dir !== d ? TURN_COST : 0;
        const ng = cur.g + 1 + turn;
        const nk = cellKey(ni, nj);
        const prev = gScore.get(nk);
        if (prev !== undefined && ng >= prev) continue;
        gScore.set(nk, ng);
        parent.set(nk, ck);
        open.push({ i: ni, j: nj, g: ng, f: ng + h(ni, nj), dir: d });
      }
    }
    return null;
  };

  // Keep caller order (solid → gateway → dashed, longer first within kind).
  for (const req of requests) {
    const pts = astar(req) || fallbackL(req);
    markPathCells(pts);
    out.set(req.id, pathDFromPoints(pts));
  }
  return out;
}

/** Build obstacle map for every placed agent. */
export function buildAgentObstacles(placed: PlacedNode[]): Map<string, Rect> {
  const m = new Map<string, Rect>();
  for (const p of placed) m.set(p.node.id, agentObstacle(p));
  return m;
}
