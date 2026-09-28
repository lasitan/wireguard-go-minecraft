import { useState } from "react";
import "./edgeLegend.css";

export function EdgeLegend() {
  const [open, setOpen] = useState(true);
  return (
    <aside className={`edge-legend${open ? " is-open" : ""}`} aria-label="线段图例">
      <button
        type="button"
        className="edge-legend-toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="edge-legend-title">线段含义</span>
        <span className="edge-legend-chevron" aria-hidden>
          {open ? "▾" : "▸"}
        </span>
      </button>
      <div className="edge-legend-body">
        <div className="edge-legend-body-inner">
          <ul className="edge-legend-list">
            <li>
              <span className="swatch line solid green" />
              <span>实线 · 两者之间有规则在运行</span>
            </li>
            <li>
              <span className="swatch line gateway green" />
              <span>点线 · 跨网段网关（目标网段内延迟最低的母卡）</span>
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
            <li>
              <span className="swatch line solid grey" />
              <span>灰色 · 节点已停用</span>
            </li>
          </ul>
          <p className="edge-legend-note">
            拖动画布平移 · 滚轮缩放 · 拖拽 Agent 改位置。虚实互斥；同 IP 仅前端标黄。
          </p>
        </div>
      </div>
    </aside>
  );
}
