import { useState } from "react";
import type { Node, NodeStats } from "../../core/models";
import { state } from "../../core/state";
import { RELAY_ID } from "../../core/constants";
import { patchNode } from "../../app/NodeActions";
import { shortName } from "../../utils/shortName";
import { validateCidr } from "../../utils/validateCidr";

export function RoutesTab({
  node,
  stats,
  onError,
}: {
  node: Node;
  stats: NodeStats | null;
  onError: (msg: string) => void;
}) {
  const routes = node.routes ?? [];
  const gateways = state.mesh?.paths?.[node.id] ?? {};
  const byId = new Map((state.mesh?.nodes ?? []).map((n) => [n.id, n]));
  const rttOf = new Map((stats?.peers || []).filter((p) => p.nodeId && p.rttMs).map((p) => [p.nodeId!, p.rttMs!]));
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const trimmed = draft.trim();
  const err = trimmed ? validateCidr(trimmed) || (!busy && routes.includes(trimmed) ? "已存在" : "") : "";

  const save = async (next: string[]) => {
    setBusy(true);
    try {
      await patchNode(node.id, { routes: next });
      return true;
    } catch (e) {
      onError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    if (!trimmed || err) return;
    if (await save([...routes, trimmed])) setDraft("");
  };

  return (
    <>
      <p className="tab-intro muted">
        路由表决定本机经 VPN 可访问的网段。入网时已包含自身 VPN 前缀：访问同网段目标时，本机会连接目标所在的母卡（目标本身是母卡则直连）。添加另一 VPN 网段后，本机会连接该网段内所有已暴露监听的母卡，并经延迟最低的一张打通网段（Master 自动择优切换）；该网段找不到可用母卡时，才临时经 Master 中转兜底。其他 CIDR 视为子网路由，经本网段母卡转发。
      </p>
      <div className="add-row">
        <input
          placeholder="192.168.1.0/24"
          value={draft}
          aria-invalid={!!err}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void add()}
        />
        <button type="button" className="mini" disabled={busy || !trimmed || !!err} onClick={() => void add()}>
          添加
        </button>
      </div>
      <div className={`field-err${err ? " show" : ""}`}>{err || " "}</div>

      {routes.length === 0 ? (
        <div className="muted empty-note">尚未配置路由，只有本节点自身地址可达。</div>
      ) : (
        <ul className="rule-list">
          {routes.map((r) => (
            <li key={r} className="rule-item">
              <span className="rule-badge">CIDR</span>
              <code className="rule-main">{r}</code>
              {gateways[r] === RELAY_ID ? (
                <span className="rule-via muted">经 Master（兜底）</span>
              ) : gateways[r] ? (
                <span className="rule-via muted">
                  经 {shortName(byId.get(gateways[r]) || { id: gateways[r], role: "", address: "" })}
                  {rttOf.get(gateways[r]) ? ` · ${rttOf.get(gateways[r])!.toFixed(0)} ms` : ""}
                </span>
              ) : null}
              <button
                type="button"
                className="rule-del"
                aria-label={`删除 ${r}`}
                disabled={busy}
                onClick={() => void save(routes.filter((x) => x !== r))}
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
