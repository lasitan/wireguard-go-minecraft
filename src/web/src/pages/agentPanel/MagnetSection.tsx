import { useState } from "react";
import type { Node, NodeStats } from "../../core/models";
import { state } from "../../core/state";
import { patchNode, setNodeParents } from "../../app/NodeActions";
import { focusTarget } from "../../app/FocusNav";
import { attachedMothers, isMother } from "../../topology/MagnetStacks";
import { dialEndpoint } from "../../utils/dialEndpoint";
import { shortName } from "../../utils/shortName";
import { validateEndpoint, validateListenPort } from "../../utils/validateMagnet";
import { InlineField } from "./InlineField";
import "./magnetSection.css";

/** Listen port (mother card), dial address, and which mothers this card is attached to. */
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
  const mother = isMother(node);
  const kids = state.stacks.childrenOf.get(node.id) || [];
  const parentId = state.stacks.parentOf.get(node.id) || "";
  const attached = state.mesh ? attachedMothers(state.mesh, node.id) : [];
  const mothers = nodes.filter((n) => isMother(n) && n.id !== node.id);
  const ep = dialEndpoint(node);
  const rttOf = new Map((stats?.peers || []).filter((p) => p.nodeId && p.rttMs).map((p) => [p.nodeId!, p.rttMs!]));

  const toggleParent = (id: string, on: boolean) => {
    const next = on ? [...attached, id] : attached.filter((m) => m !== id);
    void act(() => setNodeParents(node.id, next)).catch(() => undefined);
  };

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
    return act(() => patchNode(node.id, { listenPort: port }));
  };

  return (
    <section className="magnet-section">
      <div className="magnet-head">
        <span className="magnet-title">磁吸</span>
        <span key={mother ? "m" : parentId ? "c" : "f"} className={`magnet-role${mother ? " mother" : parentId ? " child" : ""}`}>
          {mother
            ? `母卡 · ${kids.length} 张子卡`
            : attached.length > 1
              ? `子卡 · ${attached.length} 张母卡`
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
              {kids.length === 0 ? <span className="muted">拖动其他卡片到本卡下方即可吸附</span> : null}
              {kids.map((id) => (
                <button key={id} type="button" className="magnet-kid" onClick={() => void focusTarget(id)}>
                  {shortName(byId.get(id) || { id, role: "", address: "" })}
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
              {mothers.length === 0 ? <span className="muted">还没有母卡</span> : null}
              {mothers.map((m) => {
                const on = attached.includes(m.id);
                const rtt = rttOf.get(m.id);
                return (
                  <label key={m.id} className={`magnet-parent${on ? " on" : ""}`}>
                    <input
                      type="checkbox"
                      checked={on}
                      disabled={busy}
                      onChange={(e) => toggleParent(m.id, e.target.checked)}
                    />
                    <span className="magnet-parent-name">{shortName(m)}</span>
                    <span className="magnet-parent-ep muted">{dialEndpoint(m) || "无地址"}</span>
                    {on && rtt ? <span className="magnet-parent-rtt">{rtt.toFixed(0)} ms</span> : null}
                    {on && m.id === parentId && attached.length > 1 ? <span className="magnet-auto">最快</span> : null}
                  </label>
                );
              })}
              {attached.some((id) => !dialEndpoint(byId.get(id) || { id, role: "", address: "" })) ? (
                <div className="magnet-warn">有母卡暂无可用连接地址，本节点还连不上它</div>
              ) : null}
            </div>
          </div>
          <p className="tiny muted magnet-hint">
            可同时吸附多张母卡：本网段流量走延迟最低的一张（Master 按隧道握手 RTT 自动切换），其余母卡也能直接把流量转发给本节点。拓扑图中按住 Ctrl/Shift 拖到母卡下方即可追加。
          </p>
        </div>
      </div>
    </section>
  );
}
