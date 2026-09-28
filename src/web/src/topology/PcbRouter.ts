import { VIEW } from "../core/constants";
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

/** Match the camera grid so traces sit on visible lines. */
const CELL = 10;
const CLEAR = 8;
const TURN_COST = 0.35;
const MAX_EXPAND = 250_000;

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
  const r = 42 + pad;
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

function pointInRect(x: number, y: number, r: Rect): boolean {
  return x >= r.minX && x <= r.maxX && y >= r.minY && y <= r.maxY;
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

type Cell = { i: number; j: number };

function cellKey(i: number, j: number): number {
  // 14 bits each → safe for ±8192 cells
  return ((i + 8192) << 14) | (j + 8192);
}

function unpackKey(k: number): Cell {
  return { i: (k >> 14) - 8192, j: (k & 16383) - 8192 };
}

/**
 * Bundle-route orthogonal PCB traces: avoid agent/Master obstacles and do not
 * cross previously placed traces (single-layer). Never falls back to a
 * crossing L-bend — if crowded, routes around the outer free channel.
 */
export function routePcbBundle(
  requests: RouteRequest[],
  obstaclesById: Map<string, Rect>,
  masterBlocked: Rect,
): Map<string, string> {
  const out = new Map<string, string>();
  if (requests.length === 0) return out;

  const hard: Rect[] = [masterBlocked];
  for (const r of obstaclesById.values()) hard.push(r);

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
  for (const r of hard) {
    grow(r.minX, r.minY);
    grow(r.maxX, r.maxY);
  }
  for (const req of requests) {
    grow(req.x1, req.y1);
    grow(req.x2, req.y2);
  }
  // Generous outer channel so later traces can always go around.
  const margin = Math.max(CELL * 48, VIEW.w * 0.35);
  minX -= margin;
  minY -= margin;
  maxX += margin;
  maxY += margin;

  // Align origin to CELL so world ↔ cell is stable.
  minX = Math.floor(minX / CELL) * CELL;
  minY = Math.floor(minY / CELL) * CELL;

  const toCell = (x: number, y: number): Cell => ({
    i: Math.round((x - minX) / CELL),
    j: Math.round((y - minY) / CELL),
  });
  const toWorld = (c: Cell): Pt => ({ x: minX + c.i * CELL, y: minY + c.j * CELL });

  const iMax = Math.ceil((maxX - minX) / CELL);
  const jMax = Math.ceil((maxY - minY) / CELL);

  const blockedCells = new Set<number>();
  for (let i = 0; i <= iMax; i++) {
    for (let j = 0; j <= jMax; j++) {
      const p = toWorld({ i, j });
      for (const r of hard) {
        if (pointInRect(p.x, p.y, r)) {
          blockedCells.add(cellKey(i, j));
          break;
        }
      }
    }
  }

  /** Cells reserved by already-routed traces (including clearance halo). */
  const reserved = new Set<number>();

  const markCells = (cells: Cell[], clearEnds: boolean) => {
    const ends = new Set<number>();
    if (clearEnds && cells.length) {
      ends.add(cellKey(cells[0].i, cells[0].j));
      ends.add(cellKey(cells[cells.length - 1].i, cells[cells.length - 1].j));
    }
    for (const c of cells) {
      const k = cellKey(c.i, c.j);
      if (!ends.has(k)) reserved.add(k);
    }
  };

  const isBlocked = (i: number, j: number, start: Cell, goal: Cell): boolean => {
    if (i < 0 || j < 0 || i > iMax || j > jMax) return true;
    const k = cellKey(i, j);
    // Reserved cells always block — shared ports are freed after marking so
    // a new edge may sit on them, but never cut through another trace's body.
    if (reserved.has(k)) return true;
    if ((i === start.i && j === start.j) || (i === goal.i && j === goal.j)) return false;
    if (blockedCells.has(k)) return true;
    return false;
  };

  const cellFree = (i: number, j: number): boolean => {
    if (i < 0 || j < 0 || i > iMax || j > jMax) return false;
    const k = cellKey(i, j);
    return !blockedCells.has(k) && !reserved.has(k);
  };

  /** Sample the ortho segment so same-cell rounding cannot hide a crossing. */
  const corridorFree = (from: Pt, to: Pt): boolean => {
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    const len = Math.hypot(dx, dy);
    if (len < 0.01) return true;
    const steps = Math.max(1, Math.ceil(len / (CELL * 0.5)));
    const fromC = toCell(from.x, from.y);
    const toC = toCell(to.x, to.y);
    for (let s = 0; s <= steps; s++) {
      const t = s / steps;
      const c = toCell(from.x + dx * t, from.y + dy * t);
      if (c.i === fromC.i && c.j === fromC.j) continue;
      if (c.i === toC.i && c.j === toC.j) continue;
      if (!cellFree(c.i, c.j)) return false;
    }
    return true;
  };

  /** Manhattan stub from port cell to snap cell must stay on free cells. */
  const stubFree = (port: Cell, snap: Cell): boolean => {
    // Prefer matching the snap's column first (vertical then horizontal), else the other L.
    const tryOrder = (firstAxis: "h" | "v"): boolean => {
      let ci = port.i;
      let cj = port.j;
      if (firstAxis === "v") {
        while (cj !== snap.j) {
          cj += snap.j > cj ? 1 : -1;
          if (!cellFree(ci, cj) && !(ci === snap.i && cj === snap.j)) return false;
        }
        while (ci !== snap.i) {
          ci += snap.i > ci ? 1 : -1;
          if (!cellFree(ci, cj) && !(ci === snap.i && cj === snap.j)) return false;
        }
      } else {
        while (ci !== snap.i) {
          ci += snap.i > ci ? 1 : -1;
          if (!cellFree(ci, cj) && !(ci === snap.i && cj === snap.j)) return false;
        }
        while (cj !== snap.j) {
          cj += snap.j > cj ? 1 : -1;
          if (!cellFree(ci, cj) && !(ci === snap.i && cj === snap.j)) return false;
        }
      }
      return true;
    };
    if (port.i === snap.i && port.j === snap.j) return true;
    return tryOrder("v") || tryOrder("h");
  };

  /** Push start/goal onto a free cell whose stub back to the port stays clear. */
  const snapPort = (x: number, y: number, towardX: number, towardY: number): Cell => {
    const port = toCell(x, y);
    const tdx = towardX - x;
    const tdy = towardY - y;
    const accept = (cand: Cell) => cellFree(cand.i, cand.j) && stubFree(port, cand);

    if (accept(port)) return port;

    const prefer: Cell[] = [];
    if (Math.abs(tdx) >= Math.abs(tdy)) {
      prefer.push({ i: port.i + (tdx >= 0 ? 1 : -1), j: port.j });
      prefer.push({ i: port.i, j: port.j + (tdy >= 0 ? 1 : -1) });
      prefer.push({ i: port.i, j: port.j + (tdy >= 0 ? -1 : 1) });
      prefer.push({ i: port.i + (tdx >= 0 ? -1 : 1), j: port.j });
    } else {
      prefer.push({ i: port.i, j: port.j + (tdy >= 0 ? 1 : -1) });
      prefer.push({ i: port.i + (tdx >= 0 ? 1 : -1), j: port.j });
      prefer.push({ i: port.i + (tdx >= 0 ? -1 : 1), j: port.j });
      prefer.push({ i: port.i, j: port.j + (tdy >= 0 ? -1 : 1) });
    }
    for (let r = 1; r <= 12; r++) {
      for (const base of prefer) {
        const cand = {
          i: port.i + (base.i - port.i) * r,
          j: port.j + (base.j - port.j) * r,
        };
        if (accept(cand)) return cand;
      }
      for (let di = -r; di <= r; di++) {
        for (let dj = -r; dj <= r; dj++) {
          if (Math.max(Math.abs(di), Math.abs(dj)) !== r) continue;
          const cand = { i: port.i + di, j: port.j + dj };
          if (accept(cand)) return cand;
        }
      }
    }
    return port;
  };

  const reconstruct = (parent: Map<number, number>, goalK: number, start: Cell): Cell[] => {
    const cells: Cell[] = [];
    let ck = goalK;
    for (;;) {
      const c = unpackKey(ck);
      cells.push(c);
      if (c.i === start.i && c.j === start.j) break;
      const pk = parent.get(ck);
      if (pk === undefined) break;
      ck = pk;
    }
    cells.reverse();
    return cells;
  };

  const astar = (start: Cell, goal: Cell): Cell[] | null => {
    if (start.i === goal.i && start.j === goal.j) return [start];

    type Node = { i: number; j: number; g: number; f: number; dir: number };
    const open: Node[] = [];
    const gScore = new Map<number, number>();
    const parent = new Map<number, number>();
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
        return reconstruct(parent, cellKey(cur.i, cur.j), start);
      }

      const ck = cellKey(cur.i, cur.j);
      const known = gScore.get(ck);
      if (known !== undefined && cur.g > known + 1e-6) continue;

      for (const { di, dj, d } of dirs) {
        const ni = cur.i + di;
        const nj = cur.j + dj;
        if (isBlocked(ni, nj, start, goal)) continue;
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

  /**
   * Detour via A* through a unique perimeter waypoint. Never walks through
   * reserved cells (unlike a naive L-bend fallback).
   */
  const outerDetour = (start: Cell, goal: Cell, laneIndex: number): Cell[] | null => {
    const stitchAstar = (waypoints: Cell[]): Cell[] | null => {
      const path: Cell[] = [];
      let cur = start;
      for (const wp of [...waypoints, goal]) {
        const seg = astar(cur, wp);
        if (!seg) return null;
        if (path.length) path.push(...seg.slice(1));
        else path.push(...seg);
        cur = wp;
      }
      return path;
    };

    const inset = 2 + (laneIndex % 24) * 2;
    const candidates: Cell[][] = [
      [{ i: inset, j: inset }],
      [{ i: iMax - inset, j: inset }],
      [{ i: inset, j: jMax - inset }],
      [{ i: iMax - inset, j: jMax - inset }],
      [
        { i: start.i, j: inset },
        { i: goal.i, j: inset },
      ],
      [
        { i: start.i, j: jMax - inset },
        { i: goal.i, j: jMax - inset },
      ],
      [
        { i: inset, j: start.j },
        { i: inset, j: goal.j },
      ],
      [
        { i: iMax - inset, j: start.j },
        { i: iMax - inset, j: goal.j },
      ],
      // Two-corner wrap (full U-turn around the board).
      [
        { i: inset, j: inset },
        { i: iMax - inset, j: inset },
        { i: iMax - inset, j: jMax - inset },
      ],
      [
        { i: iMax - inset, j: inset },
        { i: inset, j: inset },
        { i: inset, j: jMax - inset },
      ],
    ];
    // Rotate so each edge prefers a different first candidate.
    const rotated = [
      ...candidates.slice(laneIndex % candidates.length),
      ...candidates.slice(0, laneIndex % candidates.length),
    ];
    for (const wps of rotated) {
      const path = stitchAstar(wps);
      if (path) return path;
    }
    return null;
  };

  const cellsToWorld = (cells: Cell[], x1: number, y1: number, x2: number, y2: number): Pt[] => {
    if (!cells.length) return [{ x: x1, y: y1 }, { x: x2, y: y2 }];
    const expanded: Cell[] = [cells[0]];
    for (let n = 1; n < cells.length; n++) {
      const a = expanded[expanded.length - 1];
      const b = cells[n];
      if (a.i !== b.i && a.j !== b.j) expanded.push({ i: b.i, j: a.j });
      expanded.push(b);
    }
    // Stay on grid centres. Port stubs that share a rounded cell with another
    // trace would otherwise paint a geometric crossing.
    return simplifyOrthogonal(expanded.map(toWorld));
  };

  let laneIndex = 0;
  for (const req of requests) {
    const start = snapPort(req.x1, req.y1, req.x2, req.y2);
    const goal = snapPort(req.x2, req.y2, req.x1, req.y1);
    let cells = astar(start, goal);
    if (!cells) cells = outerDetour(start, goal, laneIndex++);
    if (!cells) {
      cells = outerDetour(start, goal, laneIndex + 17);
      laneIndex++;
    }
    if (!cells) cells = [start, goal];

    const worldPts = cellsToWorld(cells, req.x1, req.y1, req.x2, req.y2);

    // Rasterize the *displayed* polyline onto the grid so stubs cannot drift
    // through another trace's cells without being reserved.
    const raster: Cell[] = [];
    const addCell = (i: number, j: number) => {
      const last = raster[raster.length - 1];
      if (last && last.i === i && last.j === j) return;
      raster.push({ i, j });
    };
    for (let s = 1; s < worldPts.length; s++) {
      const a = worldPts[s - 1];
      const b = worldPts[s];
      const ca = toCell(a.x, a.y);
      const cb = toCell(b.x, b.y);
      let i = ca.i;
      let j = ca.j;
      addCell(i, j);
      while (i !== cb.i) {
        i += cb.i > i ? 1 : -1;
        addCell(i, j);
      }
      while (j !== cb.j) {
        j += cb.j > j ? 1 : -1;
        addCell(i, j);
      }
    }
    markCells(raster.length ? raster : cells, true);
    out.set(req.id, pathDFromPoints(worldPts));
  }
  return out;
}

/** Build obstacle map for every placed agent. */
export function buildAgentObstacles(placed: PlacedNode[]): Map<string, Rect> {
  const m = new Map<string, Rect>();
  for (const p of placed) m.set(p.node.id, agentObstacle(p));
  return m;
}
