import { describe, expect, it } from "vitest";
import { routePcbBundle, type Rect, type RouteRequest } from "./PcbRouter";

function parsePath(d: string): { x: number; y: number }[] {
  const pts: { x: number; y: number }[] = [];
  const re = /[ML]\s*([-\d.]+)\s+([-\d.]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(d))) pts.push({ x: +m[1], y: +m[2] });
  return pts;
}

function orthogonalOrChamfer(pts: { x: number; y: number }[]): boolean {
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1];
    const b = pts[i];
    const dx = Math.abs(a.x - b.x);
    const dy = Math.abs(a.y - b.y);
    if (dx < 0.01 || dy < 0.01) continue; // axis-aligned
    if (Math.abs(dx - dy) < 0.5 && dx <= 14) continue; // 45° PCB chamfer
    return false;
  }
  return true;
}

function segmentCells(
  a: { x: number; y: number },
  b: { x: number; y: number },
  cell = 12,
): Set<string> {
  const out = new Set<string>();
  if (Math.abs(a.x - b.x) < 0.01) {
    const y0 = Math.min(a.y, b.y);
    const y1 = Math.max(a.y, b.y);
    for (let y = y0; y <= y1 + cell * 0.5; y += cell) {
      out.add(`${Math.round(a.x / cell)},${Math.round(y / cell)}`);
    }
  } else {
    const x0 = Math.min(a.x, b.x);
    const x1 = Math.max(a.x, b.x);
    for (let x = x0; x <= x1 + cell * 0.5; x += cell) {
      out.add(`${Math.round(x / cell)},${Math.round(a.y / cell)}`);
    }
  }
  return out;
}

function pathCells(pts: { x: number; y: number }[]): Set<string> {
  const out = new Set<string>();
  for (let i = 1; i < pts.length; i++) {
    for (const c of segmentCells(pts[i - 1], pts[i])) out.add(c);
  }
  return out;
}

describe("routePcbBundle", () => {
  it("routes orthogonally around obstacles without crossing", () => {
    const obstacles = new Map<string, Rect>([
      ["a", { minX: 80, maxX: 160, minY: 80, maxY: 140 }],
      ["b", { minX: 240, maxX: 320, minY: 80, maxY: 140 }],
      ["block", { minX: 170, maxX: 230, minY: 40, maxY: 200 }],
    ]);
    const master: Rect = { minX: 400, maxX: 480, minY: 400, maxY: 480 };
    const reqs: RouteRequest[] = [
      { id: "e1", fromId: "a", toId: "b", x1: 80, y1: 110, x2: 320, y2: 110 },
      { id: "e2", fromId: "a", toId: "b", x1: 80, y1: 130, x2: 320, y2: 90 },
    ];
    const paths = routePcbBundle(reqs, obstacles, master);
    const p1 = parsePath(paths.get("e1")!);
    const p2 = parsePath(paths.get("e2")!);
    expect(p1.length).toBeGreaterThan(1);
    expect(p2.length).toBeGreaterThan(1);
    expect(orthogonalOrChamfer(p1)).toBe(true);
    expect(orthogonalOrChamfer(p2)).toBe(true);

    const c1 = pathCells(p1);
    const c2 = pathCells(p2);
    // Drop endpoints so shared ports don't count as a cross.
    const dropEnds = (pts: { x: number; y: number }[], cells: Set<string>) => {
      const a = `${Math.round(pts[0].x / 12)},${Math.round(pts[0].y / 12)}`;
      const b = `${Math.round(pts[pts.length - 1].x / 12)},${Math.round(pts[pts.length - 1].y / 12)}`;
      cells.delete(a);
      cells.delete(b);
    };
    dropEnds(p1, c1);
    dropEnds(p2, c2);
    for (const c of c1) expect(c2.has(c)).toBe(false);

    // Must not enter the middle block interior (sample mid-cells).
    for (const pts of [p1, p2]) {
      for (const pt of pts.slice(1, -1)) {
        expect(pt.x > 170 && pt.x < 230 && pt.y > 40 && pt.y < 200).toBe(false);
      }
    }
  });
});
