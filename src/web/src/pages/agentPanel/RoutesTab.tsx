import { useState } from "react";
import type { Node } from "../../core/models";
import { patchNode } from "../../app/NodeActions";
import { validateCidr } from "../../utils/validateCidr";

export function RoutesTab({ node, onError }: { node: Node; onError: (msg: string) => void }) {
  const routes = node.routes ?? [];
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
        路由表决定本机经 VPN 可访问的网段（写入出站 AllowedIPs）。入网时已包含自身 VPN 前缀；添加另一 VPN 网段后可跨网段通信。额外 CIDR 也可用于子网路由器（经本机转发时自动开启 IP 转发）。
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
