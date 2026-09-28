import { useState } from "react";
import type { Node, NodeStats } from "../../core/models";
import { state } from "../../core/state";
import { patchNode } from "../../app/NodeActions";
import { focusTarget } from "../../app/FocusNav";
import { attachedMothers, clusterMembers, isMother, servingMothers } from "../../topology/MagnetStacks";
import { dialEndpoint } from "../../utils/dialEndpoint";
import { isOnline } from "../../utils/isOnline";
import { shortName } from "../../utils/shortName";
import { validateEndpoint, validateListenPort } from "../../utils/validateMagnet";
import { InlineField } from "./InlineField";
import "./magnetSection.css";

/**
 * Listen port (mother card), dial address, and a read-only view of the card's
 * magnet relations. Attaching and clustering happen by dragging on the topology.
 */
export function MagnetSection({
  node,
  stats,
  onError,
}: {
  node: Node;
  stats: NodeStats | null;
  onError: (msg: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const nodes = state.mesh?.nodes || [];
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const nameOf = (id: string) => shortName(byId.get(id) || { id, role: "", address: "" });
  const mother = isMother(node);
  const kids = state.stacks.childrenOf.get(node.id) || [];
  const parentId = state.stacks.parentOf.get(node.id) || "";
  const attached = state.mesh ? attachedMothers(state.mesh, node.id) : [];
  const serving = state.mesh ? servingMothers(state.mesh, node.id) : [];
  const members = state.mesh && node.cluster ? clusterMembers(state.mesh, node.id) : [];
  const standby = state.mesh?.standby || {};
  const ep = dialEndpoint(node);
  const rttOf = new Map((stats?.peers || []).filter((p) => p.nodeId && p.rttMs).map((p) => [p.nodeId!, p.rttMs!]));

  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      onError((e as Error).message);
      throw e;
    } finally {
      setBusy(false);
    }
  };

  const savePort = (draft: string) => {
    const port = draft ? Number(draft) : 0;
    if (!port && kids.length && !window.confirm(`取消母卡会释放下方 ${kids.length} 张子卡，确定？`)) {
      return Promise.reject(new Error("cancelled"));
    }
    if (members.length > 1 && !window.confirm(`监听端口会同步到集群内全部 ${members.length} 台机器，确定？`)) {
      return Promise.reject(new Error("cancelled"));
    }
    return act(() => patchNode(node.id, { listenPort: port }));
  };

  return (
    <section className="magnet-section">
      <div className="magnet-head">
        <span className="magnet-title">磁吸</span>
        <span key={mother ? "m" : parentId ? "c" : "f"} className={`magnet-role${mother ? " mother" : parentId ? " child" : ""}`}>
          {mother
            ? `母卡 · ${kids.length} 张子卡`
            : serving.length > 1
              ? `子卡 · ${serving.length} 张母卡`
              : parentId
                ? "子卡"
                : "独立"}
        </span>
      </div>

      <InlineField
        label="监听端口"
        value={node.listenPort ? String(node.listenPort) : ""}
        display={node.listenPort ? <b>{node.listenPort}</b> : <b className="muted">未设置</b>}
        placeholder="如 25590；留空 = 不作为母卡"
        busy={busy}
        validate={validateListenPort}
        onSave={savePort}
      />

      <div className={`magnet-fold${mother ? " open" : ""}`}>
        <div>
          <InlineField
            label="连接地址"
            value={node.endpoint || ""}
            display={
              <>
                <b className={ep ? "" : "muted"}>{ep || "等待公网 IP…"}</b>
                {!node.endpoint ? <span className="magnet-auto">自动</span> : null}
              </>
            }
            placeholder={node.publicV4 ? `留空自动：${node.publicV4}:${node.listenPort}` : "host 或 host:端口"}
            busy={busy}
            validate={validateEndpoint}
            onSave={(draft) => act(() => patchNode(node.id, { endpoint: draft }))}
          />
          <div className="ov-row">
            <span className="ov-label">子卡</span>
            <div className="magnet-kids">
              {kids.length === 0 ? <span className="muted">暂无</span> : null}
              {kids.map((id) => (
                <button key={id} type="button" className="magnet-kid" onClick={() => void focusTarget(id)}>
                  {nameOf(id)}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      <div className={`magnet-fold${mother ? "" : " open"}`}>
        <div>
          <div className="ov-row">
            <span className="ov-label">吸附到</span>
            <div className="magnet-parents">
              {serving.length === 0 ? <span className="muted">未吸附</span> : null}
              {serving.map((id) => {
                const m = byId.get(id);
                const rtt = rttOf.get(id);
                return (
                  <button key={id} type="button" className={`magnet-parent${id === parentId ? " on" : ""}`} onClick={() => void focusTarget(id)}>
                    <span className="magnet-parent-name">{nameOf(id)}</span>
                    <span className="magnet-parent-ep muted">{m ? dialEndpoint(m) || "无地址" : ""}</span>
                    {!attached.includes(id) ? <span className="magnet-auto">集群</span> : null}
                    {rtt ? <span className="magnet-parent-rtt">{rtt.toFixed(0)} ms</span> : null}
                    {id === parentId && serving.length > 1 ? <span className="magnet-auto">当前</span> : null}
                  </button>
                );
              })}
              {serving.some((id) => !dialEndpoint(byId.get(id) || { id, role: "", address: "" })) ? (
                <div className="magnet-warn">有母卡暂无可用连接地址，本节点还连不上它</div>
              ) : null}
            </div>
          </div>
        </div>
      </div>

      {members.length > 1 ? (
        <div className="ov-row">
          <span className="ov-label">集群</span>
          <div className="magnet-kids">
            {members.map((id) => {
              const m = byId.get(id);
              const down = !!m && (m.disabled || !isOnline(m));
              return (
                <button
                  key={id}
                  type="button"
                  className={`magnet-kid${id === node.id ? " self" : ""}${down ? " down" : ""}`}
                  title={standby[id] ? `离线，已由 ${nameOf(standby[id])} 接管` : undefined}
                  onClick={() => void focusTarget(id)}
                >
                  {nameOf(id)}
                </button>
              );
            })}
          </div>
        </div>
      ) : null}

      <p className="tiny muted magnet-hint">
        磁吸只能在拓扑图中拖动卡片调整：拖到母卡下方吸附（按住 Ctrl/Shift 追加），拖出即解除；拖到同类卡片左右两侧组成集群，拖离即退出。
        集群内任一台修改监听端口、路由、吸附或端口转发都会同步到其他成员；成员离线时，子卡自动切到其他成员，指向它的端口转发也会转给在线成员。
      </p>
    </section>
  );
}
