type Pt = { x: number; y: number };

/** Bevel length at each 90° bend (two 45° legs). Keep ≤ half cell so parallel traces stay clear. */
export const PCB_CHAMFER = 4;

function dedupeColinear(pts: Pt[]): Pt[] {
  if (pts.length <= 2) return pts;
  const out: Pt[] = [pts[0]];
  for (let i = 1; i < pts.length; i++) {
    const prev = out[out.length - 1];
    const cur = pts[i];
    if (Math.hypot(cur.x - prev.x, cur.y - prev.y) < 0.001) continue;
    out.push(cur);
  }
  return out;
}

function unit(ax: number, ay: number): Pt {
  const len = Math.hypot(ax, ay);
  if (len < 1e-6) return { x: 0, y: 0 };
  return { x: ax / len, y: ay / len };
}

/** Replace sharp corners with two 45° segments meeting at the bevel midpoint. */
export function chamferOrthogonal(pts: Pt[], chamfer = PCB_CHAMFER): Pt[] {
  const p = dedupeColinear(pts);
  if (p.length <= 2) return p;

  const out: Pt[] = [p[0]];
  for (let i = 1; i < p.length - 1; i++) {
    const prev = p[i - 1];
    const corner = p[i];
    const next = p[i + 1];
    const u = unit(corner.x - prev.x, corner.y - prev.y);
    const v = unit(next.x - corner.x, next.y - corner.y);
    const lenIn = Math.hypot(corner.x - prev.x, corner.y - prev.y);
    const lenOut = Math.hypot(next.x - corner.x, next.y - corner.y);
    const r = Math.min(chamfer, lenIn * 0.48, lenOut * 0.48);
    if (r < 2 || Math.abs(u.x * v.x + u.y * v.y) > 0.05) {
      out.push(corner);
      continue;
    }
    const a = { x: corner.x - u.x * r, y: corner.y - u.y * r };
    const b = { x: corner.x + v.x * r, y: corner.y + v.y * r };
    const m = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    out.push(a, m, b);
  }
  out.push(p[p.length - 1]);
  return dedupeColinear(out);
}

export function pathDFromPoints(pts: Pt[]): string {
  const p = chamferOrthogonal(pts);
  if (p.length < 2) return "";
  return `M ${p[0].x} ${p[0].y}` + p.slice(1).map((q) => ` L ${q.x} ${q.y}`).join("");
}

/** L-shaped PCB trace; longer axis first, straight line when aligned. */
export function pcbRoutePoints(x1: number, y1: number, x2: number, y2: number): Pt[] {
  const dx = Math.abs(x2 - x1);
  const dy = Math.abs(y2 - y1);
  if (dx < 0.001 || dy < 0.001) return [{ x: x1, y: y1 }, { x: x2, y: y2 }];
  if (dx >= dy) return [{ x: x1, y: y1 }, { x: x2, y: y1 }, { x: x2, y: y2 }];
  return [{ x: x1, y: y1 }, { x: x1, y: y2 }, { x: x2, y: y2 }];
}

/** Vertical stack jog: down — across — down (magnet rails). */
export function pcbStackRoutePoints(x1: number, y1: number, x2: number, y2: number): Pt[] {
  if (Math.abs(x2 - x1) < 0.001) return [{ x: x1, y: y1 }, { x: x2, y: y2 }];
  const midY = (y1 + y2) / 2;
  return [
    { x: x1, y: y1 },
    { x: x1, y: midY },
    { x: x2, y: midY },
    { x: x2, y: y2 },
  ];
}

export function pcbRouteD(x1: number, y1: number, x2: number, y2: number): string {
  return pathDFromPoints(pcbRoutePoints(x1, y1, x2, y2));
}

export function pcbStackRouteD(x1: number, y1: number, x2: number, y2: number): string {
  return pathDFromPoints(pcbStackRoutePoints(x1, y1, x2, y2));
}
