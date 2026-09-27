import { useState } from "react";
import type { Node } from "../../core/models";
import { state } from "../../core/state";
import { attachNode, patchNode } from "../../app/NodeActions";
import { focusTarget } from "../../app/FocusNav";
import { isMother } from "../../topology/MagnetStacks";
import { dialEndpoint } from "../../utils/dialEndpoint";
import { shortName } from "../../utils/shortName";
import { validateEndpoint, validateListenPort } from "../../utils/validateMagnet";
import { InlineField } from "./InlineField";
import "./magnetSection.css";

/** Listen port (mother card), dial address, and which mother this card is attached to. */
export function MagnetSection({ node, onError }: { node: Node; onError: (msg: string) => void }) {
  const [busy, setBusy] = useState(false);
  const nodes = state.mesh?.nodes || [];
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const mother = isMother(node);
  const kids = state.stacks.childrenOf.get(node.id) || [];
  const parentId = state.stacks.parentOf.get(node.id) || "";
  const parent = byId.get(parentId);
  const mothers = nodes.filter((n) => isMother(n) && n.id !== node.id);
  const ep = dialEndpoint(node);

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
          {mother ? `母卡 · ${kids.length} 张子卡` : parentId ? "子卡" : "独立"}
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
            <div>
              <select
                value={parentId}
                disabled={busy}
                aria-label="吸附到母卡"
                onChange={(e) => void act(() => attachNode(node.id, e.target.value)).catch(() => undefined)}
              >
                <option value="">不吸附</option>
                {mothers.map((m) => (
                  <option key={m.id} value={m.id}>
                    {shortName(m)} · {dialEndpoint(m) || "无地址"}
                  </option>
                ))}
              </select>
              {parent && !dialEndpoint(parent) ? (
                <div className="magnet-warn">母卡暂无可用连接地址，本节点还连不上它</div>
              ) : null}
            </div>
          </div>
          <p className="tiny muted magnet-hint">
            吸附后 Master 会把母卡的连接地址下发给本节点，自动建立连接；换一张母卡即自动切换。也可以直接在拓扑图里把卡片拖到母卡下方。
          </p>
        </div>
      </div>
    </section>
  );
}
