import { useMemo, useState } from "react";
import type { TrafficPoint } from "../../core/models";
import { formatBytes } from "../../utils/formatBytes";
import { formatRate } from "../../utils/formatRate";

const W = 320;
const H = 120;
const PAD_TOP = 8;

function pathFor(points: TrafficPoint[], key: "rx" | "tx", max: number, close: boolean): string {
  if (points.length === 0) return "";
  const n = points.length;
  const x = (i: number) => (n === 1 ? W / 2 : (i / (n - 1)) * W);
  const y = (v: number) => H - (v / max) * (H - PAD_TOP);
  let d = `M${x(0).toFixed(2)},${y(points[0][key]).toFixed(2)}`;
  for (let i = 1; i < n; i++) d += `L${x(i).toFixed(2)},${y(points[i][key]).toFixed(2)}`;
  if (close) d += `L${x(n - 1).toFixed(2)},${H}L${x(0).toFixed(2)},${H}Z`;
  return d;
}

function formatTick(ts: number, step: number): string {
  const d = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return step >= 3600 ? `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}时` : `${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** Two-series area chart: ↓ rx (green) and ↑ tx (orange), bytes per bucket. */
export function TrafficChart({ points, step }: { points: TrafficPoint[]; step: number }) {
  const [hover, setHover] = useState<number | null>(null);
  const max = useMemo(() => Math.max(1, ...points.map((p) => Math.max(p.rx, p.tx))) * 1.08, [points]);
  const hp = hover != null ? points[hover] : null;

  if (points.length === 0) {
    return <div className="chart-empty muted">该时间段内暂无流量数据</div>;
  }

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const t = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width));
    setHover(Math.round(t * (points.length - 1)));
  };
  const hoverX = hover != null && points.length > 1 ? (hover / (points.length - 1)) * 100 : 50;

  return (
    <div className="chart">
      <div className="chart-peak tiny muted">峰值 {formatRate(max / 1.08 / step)}</div>
      <div className="chart-box">
        <svg
          viewBox={`0 0 ${W} ${H}`}
          preserveAspectRatio="none"
          onPointerMove={onMove}
          onPointerLeave={() => setHover(null)}
        >
          <line className="chart-grid" x1={0} x2={W} y1={H / 2} y2={H / 2} vectorEffect="non-scaling-stroke" />
          <path className="chart-area rx" d={pathFor(points, "rx", max, true)} />
          <path className="chart-area tx" d={pathFor(points, "tx", max, true)} />
          <path className="chart-line rx" d={pathFor(points, "rx", max, false)} vectorEffect="non-scaling-stroke" />
          <path className="chart-line tx" d={pathFor(points, "tx", max, false)} vectorEffect="non-scaling-stroke" />
        </svg>
        <div className={`chart-cursor${hp ? " show" : ""}`} style={{ left: `${hoverX}%` }} />
        {hp ? (
          <div className={`chart-tip${hoverX > 60 ? " left" : ""}`} style={{ left: `${hoverX}%` }}>
            <div className="tiny muted">{formatTick(hp.ts, step)}</div>
            <div className="tip-rx">↓ {formatBytes(hp.rx)} · {formatRate(hp.rx / step)}</div>
            <div className="tip-tx">↑ {formatBytes(hp.tx)} · {formatRate(hp.tx / step)}</div>
          </div>
        ) : null}
      </div>
      <div className="chart-axis tiny muted">
        <span>{formatTick(points[0].ts, step)}</span>
        <span>{formatTick(points[points.length - 1].ts, step)}</span>
      </div>
      <div className="chart-legend tiny">
        <span className="lg rx">下行</span>
        <span className="lg tx">上行</span>
      </div>
    </div>
  );
}
