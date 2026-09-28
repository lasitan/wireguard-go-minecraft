import { useEffect, useRef, useState } from "react";
import type { Node } from "../../core/models";
import { goHome } from "../../app/FocusNav";
import { shortName } from "../../utils/shortName";
import { ForwardsTab } from "./ForwardsTab";
import { OverviewTab } from "./OverviewTab";
import { RoutesTab } from "./RoutesTab";
import { TrafficTab } from "./TrafficTab";
import { useNodeStats } from "./useNodeStats";
import "./agentPanel.css";

const TABS = [
  { id: "overview", label: "概览" },
  { id: "traffic", label: "流量" },
  { id: "routes", label: "路由" },
  { id: "forwards", label: "转发" },
] as const;
type TabId = (typeof TABS)[number]["id"];

const TAB_FADE_MS = 140;

const LINK_LABEL = { ws: "长连接", http: "HTTP 轮询", offline: "离线" } as const;

export function AgentPanel({ node, conflict }: { node: Node; conflict: boolean }) {
  const [tab, setTab] = useState<TabId>("overview");
  const [shown, setShown] = useState<TabId>("overview");
  const [leaving, setLeaving] = useState(false);
  const [toast, setToast] = useState("");
  const fadeTimer = useRef(0);
  const toastTimer = useRef(0);
  const { stats, error } = useNodeStats(node.id);

  useEffect(() => {
    setTab("overview");
    setShown("overview");
    setLeaving(false);
  }, [node.id]);

  useEffect(
    () => () => {
      window.clearTimeout(fadeTimer.current);
      window.clearTimeout(toastTimer.current);
    },
    [],
  );

  const switchTab = (next: TabId) => {
    if (next === tab) return;
    setTab(next);
    setLeaving(true);
    window.clearTimeout(fadeTimer.current);
    fadeTimer.current = window.setTimeout(() => {
      setShown(next);
      setLeaving(false);
    }, TAB_FADE_MS);
  };

  const report = (msg: string) => {
    setToast(msg);
    window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast(""), 4000);
  };

  const link = stats?.link ?? "offline";
  const tabIndex = TABS.findIndex((t) => t.id === tab);

  return (
    <div className="agent-panel">
      <div className="drawer-head">
        <div>
          <div className="drawer-kicker">
            {node.role}
            {node.disabled ? " · 已停用" : conflict ? " · 冲突" : ""}
          </div>
          <h2>{node.name || shortName(node)}</h2>
          <span className={`link-badge link-${link}`}>
            <span className="link-dot" />
            {LINK_LABEL[link]}
            {stats?.rttMs ? ` · ${stats.rttMs.toFixed(0)} ms` : ""}
          </span>
        </div>
        <button type="button" className="icon-btn" aria-label="关闭" onClick={() => void goHome()}>
          ✕
        </button>
      </div>

      <div className="panel-tabs" role="tablist" style={{ ["--tab-count" as string]: TABS.length }}>
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={`panel-tab${tab === t.id ? " active" : ""}`}
            onClick={() => switchTab(t.id)}
          >
            {t.label}
          </button>
        ))}
        <span className="panel-tab-ink" style={{ transform: `translateX(${tabIndex * 100}%)` }} />
      </div>

      <div className={`toast${toast || error ? " show" : ""}`} role="status">
        {toast || (error ? `统计获取失败：${error}` : "")}
      </div>

      <div className={`drawer-body tab-pane${leaving ? " leaving" : ""}`} key={shown}>
        {shown === "overview" ? <OverviewTab node={node} stats={stats} conflict={conflict} onError={report} /> : null}
        {shown === "traffic" ? <TrafficTab node={node} stats={stats} /> : null}
        {shown === "routes" ? <RoutesTab node={node} stats={stats} onError={report} /> : null}
        {shown === "forwards" ? <ForwardsTab node={node} stats={stats} onError={report} /> : null}
      </div>
    </div>
  );
}
