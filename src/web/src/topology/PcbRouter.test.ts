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
    if (dx < 0.01 || dy < 0.01) continue;
    if (Math.abs(dx - dy) < 0.5 && dx <= 6) continue;
    return false;
  }
  return true;
}

/** True if two axis-aligned open segments properly intersect (not mere endpoint touch). */
function segmentsCross(
  a1: { x: number; y: number },
  a2: { x: number; y: number },
  b1: { x: number; y: number },
  b2: { x: number; y: number },
): boolean {
  const aH = Math.abs(a1.y - a2.y) < 0.01;
  const bH = Math.abs(b1.y - b2.y) < 0.01;
  if (aH === bH) return false; // parallel
  const h1 = aH ? a1 : b1;
  const h2 = aH ? a2 : b2;
  const v1 = aH ? b1 : a1;
  const v2 = aH ? b2 : a2;
  const y = h1.y;
  const x = v1.x;
  const xLo = Math.min(h1.x, h2.x) + 0.5;
  const xHi = Math.max(h1.x, h2.x) - 0.5;
  const yLo = Math.min(v1.y, v2.y) + 0.5;
  const yHi = Math.max(v1.y, v2.y) - 0.5;
  return x > xLo && x < xHi && y > yLo && y < yHi;
}

function pathsCross(a: { x: number; y: number }[], b: { x: number; y: number }[]): boolean {
  for (let i = 1; i < a.length; i++) {
    for (let j = 1; j < b.length; j++) {
      if (segmentsCross(a[i - 1], a[i], b[j - 1], b[j])) return true;
    }
  }
  return false;
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
      { id: "e1", fromId: "a", toId: "b", x1: 72, y1: 110, x2: 328, y2: 110 },
      { id: "e2", fromId: "a", toId: "b", x1: 72, y1: 130, x2: 328, y2: 90 },
    ];
    const paths = routePcbBundle(reqs, obstacles, master);
    const p1 = parsePath(paths.get("e1")!);
    const p2 = parsePath(paths.get("e2")!);
    expect(p1.length).toBeGreaterThan(1);
    expect(p2.length).toBeGreaterThan(1);
    expect(orthogonalOrChamfer(p1)).toBe(true);
    expect(orthogonalOrChamfer(p2)).toBe(true);
    if (pathsCross(p1, p2)) {
      // eslint-disable-next-line no-console
      console.log("CROSS p1", paths.get("e1"));
      // eslint-disable-next-line no-console
      console.log("CROSS p2", paths.get("e2"));
      for (let i = 1; i < p1.length; i++) {
        for (let j = 1; j < p2.length; j++) {
          if (segmentsCross(p1[i - 1], p1[i], p2[j - 1], p2[j])) {
            // eslint-disable-next-line no-console
            console.log("seg", p1[i - 1], p1[i], "x", p2[j - 1], p2[j]);
          }
        }
      }
    }
    expect(pathsCross(p1, p2)).toBe(false);

    for (const pts of [p1, p2]) {
      for (const pt of pts.slice(1, -1)) {
        expect(pt.x > 170 && pt.x < 230 && pt.y > 40 && pt.y < 200).toBe(false);
      }
    }
  });
});
