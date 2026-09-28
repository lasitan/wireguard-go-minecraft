import { useState } from "react";
import type { VersionInfo } from "../../core/models";
import { state } from "../../core/state";
import { focusTarget } from "../../app/FocusNav";
import { recheckVersion } from "../../app/VersionCheck";
import { formatDateTime, formatRelativeTime } from "../../utils/formatRelativeTime";
import { MASTER_KEY } from "../../app/UpgradeActions";
import { UpgradeButton } from "../upgrade/UpgradeButton";
import { UpgradeStatusText } from "../upgrade/UpgradeStatusText";
import { UpdateCommandBox } from "./UpdateCommandBox";

export function UpdateCard() {
  const v = state.version;
  const [checking, setChecking] = useState(false);
  const [err, setErr] = useState("");

  const recheck = async () => {
    setChecking(true);
    setErr("");
    try {
      await recheckVersion();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setChecking(false);
    }
  };

  if (!v) {
    return <div className="update-card loading muted">正在读取版本信息…</div>;
  }

  return (
    <section className={`update-card${v.hasUpdate ? " has-update" : ""}`}>
      <div className="update-head">
        <div>
          <div className="update-kicker">当前版本</div>
          <b className="update-version">v{v.current || "?"}</b>
        </div>
        <StatusBadge v={v} />
      </div>

      <div className={`update-reveal${v.hasUpdate && v.release ? " show" : ""}`}>
        <div>
          {v.release ? (
            <p className="update-release">
              <a href={v.release.url} target="_blank" rel="noreferrer">
                {v.release.tag} 发行说明 ↗
              </a>
              <span className="muted"> · 发布于 {formatRelativeTime(v.release.publishedAt)}</span>
            </p>
          ) : null}
          <div className="upgrade-row">
            <UpgradeButton label="一键升级 Master" req={{ master: true }} keys={[MASTER_KEY]} />
            <span className="tiny muted">下载新版并重启 Master 服务，页面会自动刷新</span>
          </div>
          <UpgradeStatusText status={state.upgrades[MASTER_KEY]} />
          <p className="tiny muted upgrade-manual">也可在服务器上手动执行：</p>
          <UpdateCommandBox commands={v.commands} />
        </div>
      </div>

      <OutdatedAgents v={v} />

      <div className="update-foot">
        <span className="tiny muted" title={v.checkedAt ? formatDateTime(v.checkedAt) : ""}>
          {v.disabled
            ? "已在配置中关闭自动检测"
            : v.checkedAt
              ? `上次检测 ${formatRelativeTime(v.checkedAt)}`
              : "尚未检测"}
        </span>
        <span className="update-actions">
          {!v.hasUpdate ? (
            <a className="tiny" href={v.releasesUrl} target="_blank" rel="noreferrer">
              全部版本 ↗
            </a>
          ) : null}
          <button type="button" className="mini ghost" disabled={checking} onClick={() => void recheck()}>
            <span className={`spin-dot${checking ? " on" : ""}`} aria-hidden />
            {checking ? "检测中" : "检查更新"}
          </button>
        </span>
      </div>
      <div className={`field-err${err || v.error ? " show" : ""}`}>{err || (v.error ? `检测失败：${v.error}` : " ")}</div>
    </section>
  );
}

function StatusBadge({ v }: { v: VersionInfo }) {
  if (v.hasUpdate) return <span className="update-badge new">可升级到 v{v.latest}</span>;
  if (v.latest) return <span className="update-badge ok">已是最新</span>;
  if (v.error) return <span className="update-badge muted">检测失败</span>;
  return <span className="update-badge muted">{v.disabled ? "未启用" : "检测中"}</span>;
}

function OutdatedAgents({ v }: { v: VersionInfo }) {
  const list = v.outdatedAgents || [];
  const ids = list.map((a) => a.nodeId);
  return (
    <div className={`update-reveal${list.length ? " show" : ""}`}>
      <div>
        <div className="outdated-head-row">
          <div className="outdated-head">{list.length} 个在线 Agent 版本较旧</div>
          {list.length > 1 ? <UpgradeButton label="全部升级" req={{ outdated: true }} keys={ids} ghost /> : null}
        </div>
        <ul className="outdated-list">
          {list.map((a) => (
            <li key={a.nodeId}>
              <div className="outdated-line">
                <button type="button" className="outdated-item" onClick={() => void focusTarget(a.nodeId)}>
                  <span className="outdated-name">{a.name}</span>
                  <span className="outdated-ver">{a.version ? `v${a.version}` : "旧版"}</span>
                </button>
                <UpgradeButton
                  label="升级"
                  req={{ nodeIds: [a.nodeId] }}
                  keys={[a.nodeId]}
                  knownVersions={{ [a.nodeId]: a.version }}
                  ghost
                />
              </div>
              <UpgradeStatusText status={state.upgrades[a.nodeId]} />
            </li>
          ))}
        </ul>
        <p className="tiny muted">「升级」由 Master 远程下发；不支持远程升级的旧版 Agent，请在该机器上执行上方「已安装」命令。</p>
      </div>
    </div>
  );
}
