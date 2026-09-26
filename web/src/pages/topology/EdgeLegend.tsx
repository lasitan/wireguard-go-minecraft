import "./edgeLegend.css";

export function EdgeLegend() {
  return (
    <aside className="edge-legend" aria-label="线段图例">
      <div className="edge-legend-title">线段含义</div>
      <ul className="edge-legend-list">
        <li>
          <span className="swatch line solid green" />
          <span>实线 · 两者之间有规则在运行</span>
        </li>
        <li>
          <span className="swatch line dashed green" />
          <span>虚线 · 间接可达（无直接规则）</span>
        </li>
        <li>
          <span className="swatch line solid green flowing" />
          <span>绿色流动 · 状态正常，数据向 Master 交互</span>
        </li>
        <li>
          <span className="swatch line solid yellow" />
          <span>黄色 · 告警 / 同 IP 互斥冲突</span>
        </li>
        <li>
          <span className="swatch line solid red" />
          <span>红色 · 端点离线或链路异常</span>
        </li>
      </ul>
      <p className="edge-legend-note">虚实互斥；同 IP 仅前端标黄，不影响已运行隧道。</p>
    </aside>
  );
}
