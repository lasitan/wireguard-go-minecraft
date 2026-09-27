import { useEffect, useState } from "react";
import type { Node, NodeStats } from "../../core/models";
import { notify, state } from "../../core/state";
import { patchNode } from "../../app/NodeActions";
import { removeNode } from "../../app/Session";
import { countryFlag } from "../../utils/countryFlag";
import { formatDateTime, formatRelativeTime } from "../../utils/formatRelativeTime";
import { formatRate } from "../../utils/formatRate";
import { hostOf } from "../../utils/hostOf";
import { isOnline } from "../../utils/isOnline";
import { validateNodeAddress } from "../../utils/validateCidr";
import { AnimatedNumber } from "./AnimatedNumber";
import { Toggle } from "./Toggle";

export function OverviewTab({
  node,
  stats,
  conflict,
  onError,
}: {
  node: Node;
  stats: NodeStats | null;
  conflict: boolean;
  onError: (msg: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const online = isOnline(node);
  const status = node.disabled ? "已停用" : conflict ? "IP 冲突" : online ? "在线" : "离线";
  const statusClass = node.disabled ? "muted" : conflict ? "warn" : online ? "ok" : "muted";

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const lastSeen = stats?.lastSeen || node.lastSeen;
  const geoCode = stats?.geoCountryCode || node.geoCountryCode;
  const geoName = stats?.geoCountry || node.geoCountry;
  const v4 = stats?.publicV4 || node.publicV4;
  const v6 = stats?.publicV6 || node.publicV6;

  return (
    <>
      <div className="ov-row">
        <span className="ov-label">状态</span>
        <b className={`status-text ${statusClass}`}>{status}</b>
      </div>
      <div className="ov-row">
        <span className="ov-label">启用</span>
        <div className="ov-inline">
          <Toggle
            label="启用节点"
            checked={!node.disabled}
            disabled={busy}
            onChange={(next) => void run(() => patchNode(node.id, { enabled: next }))}
          />
          <span className="muted">{node.disabled ? "隧道已关闭，控制连接保留" : "隧道运行中"}</span>
        </div>
      </div>
      <AddressEditor node={node} busy={busy} run={run} />

      <div className="speed-grid">
        <div className="speed-card up">
          <span className="speed-label">↑ 上行</span>
          <b>
            <AnimatedNumber value={stats?.txRate ?? 0} format={formatRate} />
          </b>
        </div>
        <div className="speed-card down">
          <span className="speed-label">↓ 下行</span>
          <b>
            <AnimatedNumber value={stats?.rxRate ?? 0} format={formatRate} />
          </b>
        </div>
      </div>

      <div className="ov-row">
        <span className="ov-label">最后活跃</span>
        <div>
          <b>{formatRelativeTime(lastSeen)}</b>
          <div className="tiny muted">{formatDateTime(lastSeen)}</div>
        </div>
      </div>
      <div className="ov-row">
        <span className="ov-label">边缘位置</span>
        <div>
          {geoCode ? (
            <b className="geo">
              {countryFlag(geoCode) ? (
                <span className="geo-flag" aria-hidden>
                  {countryFlag(geoCode)}
                </span>
              ) : null}
              {geoName || geoCode}
              <span className="geo-code">{geoCode}</span>
            </b>
          ) : (
            <b className="muted">未知</b>
          )}
          <div className="tiny muted">IPv4 {v4 || "—"}</div>
          <div className="tiny muted">IPv6 {v6 || "—"}</div>
        </div>
      </div>
      <div className="ov-row">
        <span className="ov-label">控制链路</span>
        <div>
          <b>{stats?.link === "ws" ? "WebSocket 二进制" : stats?.link === "http" ? "HTTP 轮询（旧版）" : "未连接"}</b>
          {stats?.connectedAt ? <div className="tiny muted">连接于 {formatDateTime(stats.connectedAt)}</div> : null}
        </div>
      </div>
      <div className="ov-row">
        <span className="ov-label">Endpoint</span>
        <b>{node.endpoint || "—"}</b>
      </div>
      <div className="ov-row">
        <span className="ov-label">UUID</span>
        <code className="tiny">{node.id}</code>
      </div>
      <div className="ov-row">
        <span className="ov-label">Token</span>
        <code className="tiny">{node.token || ""}</code>
      </div>
      {conflict && !node.disabled ? (
        <p className="warn-note">
          与另一节点共用 {hostOf(node.address)}，且本节点改地址较晚：Master 不会把它下发给其他节点，已在运行的一方不受影响。
        </p>
      ) : null}
      <button
        type="button"
        className="danger"
        onClick={() => {
          removeNode(node.id).catch((e) => {
            state.err = e.message;
            notify();
          });
        }}
      >
        删除节点
      </button>
    </>
  );
}

function AddressEditor({
  node,
  busy,
  run,
}: {
  node: Node;
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(node.address);
  const err = editing ? validateNodeAddress(draft) : "";

  useEffect(() => {
    setEditing(false);
    setDraft(node.address);
  }, [node.id, node.address]);

  const save = () => {
    if (err) return;
    if (draft.trim() === node.address) {
      setEditing(false);
      return;
    }
    void run(async () => {
      await patchNode(node.id, { address: draft.trim() });
      setEditing(false);
    });
  };

  return (
    <div className="ov-row">
      <span className="ov-label">VPN 地址</span>
      <div className={`inline-edit${editing ? " editing" : ""}`}>
        {editing ? (
          <>
            <input
              autoFocus
              value={draft}
              aria-invalid={!!err}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") save();
                if (e.key === "Escape") {
                  setEditing(false);
                  setDraft(node.address);
                }
              }}
            />
            <div className="inline-actions">
              <button type="button" className="mini" disabled={busy || !!err} onClick={save}>
                保存
              </button>
              <button
                type="button"
                className="mini ghost"
                onClick={() => {
                  setEditing(false);
                  setDraft(node.address);
                }}
              >
                取消
              </button>
            </div>
            <div className={`field-err${err ? " show" : ""}`}>{err || " "}</div>
          </>
        ) : (
          <button type="button" className="inline-value" onClick={() => setEditing(true)} title="点击编辑">
            <b>{node.address || "—"}</b>
            <span className="edit-hint">编辑</span>
          </button>
        )}
      </div>
    </div>
  );
}
