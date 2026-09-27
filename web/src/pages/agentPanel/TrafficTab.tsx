import { useEffect, useMemo, useState } from "react";
import type { IpTraffic, Node, NodeStats, TrafficRange, TrafficSeries } from "../../core/models";
import { fetchNodeTraffic } from "../../app/NodeActions";
import { formatBytes } from "../../utils/formatBytes";
import { formatRate } from "../../utils/formatRate";
import { AnimatedNumber } from "./AnimatedNumber";
import { TrafficChart } from "./TrafficChart";

const RANGES: TrafficRange[] = ["1h", "24h", "7d", "30d"];
const SERIES_REFRESH_MS = 30_000;

type SortKey = "total" | "rx" | "tx" | "rate";

function sortValue(r: IpTraffic, k: SortKey): number {
  switch (k) {
    case "rx":
      return r.rx;
    case "tx":
      return r.tx;
    case "rate":
      return r.rxRate + r.txRate;
    default:
      return r.rx + r.tx;
  }
}

export function TrafficTab({ node, stats }: { node: Node; stats: NodeStats | null }) {
  const [range, setRange] = useState<TrafficRange>("1h");
  const [series, setSeries] = useState<TrafficSeries | null>(null);
  const [err, setErr] = useState("");
  const [sort, setSort] = useState<SortKey>("total");

  useEffect(() => {
    let alive = true;
    const load = () =>
      fetchNodeTraffic(node.id, range)
        .then((s) => {
          if (!alive) return;
          setSeries(s);
          setErr("");
        })
        .catch((e: Error) => alive && setErr(e.message));
    load();
    const t = window.setInterval(load, SERIES_REFRESH_MS);
    return () => {
      alive = false;
      window.clearInterval(t);
    };
  }, [node.id, range]);

  const rows = useMemo(
    () => [...(stats?.ips ?? [])].sort((a, b) => sortValue(b, sort) - sortValue(a, sort)),
    [stats, sort],
  );

  const th = (k: SortKey, label: string) => (
    <th>
      <button type="button" className={`th-sort${sort === k ? " active" : ""}`} onClick={() => setSort(k)}>
        {label}
        <span className="sort-caret">▾</span>
      </button>
    </th>
  );

  return (
    <>
      <div className="speed-grid">
        <div className="speed-card up">
          <span className="speed-label">累计上行</span>
          <b>
            <AnimatedNumber value={stats?.totals.tx ?? 0} format={formatBytes} />
          </b>
        </div>
        <div className="speed-card down">
          <span className="speed-label">累计下行</span>
          <b>
            <AnimatedNumber value={stats?.totals.rx ?? 0} format={formatBytes} />
          </b>
        </div>
      </div>

      <div className="section-head">
        <span>历史曲线</span>
        <div className="segmented" style={{ ["--seg-count" as string]: RANGES.length }}>
          {RANGES.map((r) => (
            <button key={r} type="button" className={range === r ? "active" : ""} onClick={() => setRange(r)}>
              {r}
            </button>
          ))}
          <span className="seg-ink" style={{ transform: `translateX(${RANGES.indexOf(range) * 100}%)` }} />
        </div>
      </div>
      {err ? <div className="field-err show">{err}</div> : null}
      <div className="chart-fade" key={range}>
        {series ? <TrafficChart points={series.points} step={series.step} /> : <div className="chart-empty muted">加载中…</div>}
      </div>

      <div className="section-head">
        <span>内网 IP 流量</span>
        <span className="tiny muted">{rows.length} 个地址</span>
      </div>
      {rows.length === 0 ? (
        <div className="muted empty-note">暂无与其他内网地址的流量</div>
      ) : (
        <table className="ip-table">
          <thead>
            <tr>
              <th>对端</th>
              {th("rx", "↓ 接收")}
              {th("tx", "↑ 发送")}
              {th("rate", "速率")}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.ip}>
                <td>
                  <div className="peer-name">{r.name || (r.nodeId ? "未命名节点" : "外部地址")}</div>
                  <div className="tiny muted">{r.ip}</div>
                </td>
                <td>{formatBytes(r.rx)}</td>
                <td>{formatBytes(r.tx)}</td>
                <td className="rate-cell">
                  <AnimatedNumber value={r.rxRate + r.txRate} format={formatRate} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
