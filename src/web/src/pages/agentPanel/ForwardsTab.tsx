import { useMemo, useState } from "react";
import type { Forward, LiveForward, Node, NodeStats } from "../../core/models";
import { state } from "../../core/state";
import { saveForwards } from "../../app/NodeActions";
import { formatBytes } from "../../utils/formatBytes";
import { hostKey } from "../../utils/hostOf";
import { isIPv4 } from "../../utils/validateCidr";
import { shortName } from "../../utils/shortName";

function validListen(s: string): string {
  const v = s.trim();
  if (!v) return "请填写监听端口";
  let port = v;
  if (v.includes(":")) {
    const [host, p] = v.split(":");
    if (!isIPv4(host)) return "监听格式为 端口 或 IPv4:端口";
    port = p;
  }
  const n = Number(port);
  if (!Number.isInteger(n) || n < 1 || n > 65535) return "端口范围 1-65535";
  return "";
}

function liveFor(f: Forward, live: LiveForward[] | undefined): LiveForward | undefined {
  return live?.find((l) => l.protocol === f.protocol && l.listen === f.listen);
}

export function ForwardsTab({
  node,
  stats,
  onError,
}: {
  node: Node;
  stats: NodeStats | null;
  onError: (msg: string) => void;
}) {
  const mesh = state.mesh;
  const forwards = useMemo(() => (mesh?.forwards ?? []).filter((f) => f.nodeId === node.id), [mesh, node.id]);
  const targets = (mesh?.nodes ?? []).filter((n) => n.id !== node.id);
  const nodeName = (id: string) => {
    const n = mesh?.nodes.find((x) => x.id === id);
    return n ? `${n.name || shortName(n)}` : "未知节点";
  };

  const [proto, setProto] = useState("tcp");
  const [listen, setListen] = useState("");
  const [dest, setDest] = useState(targets[0]?.id ?? "");
  const [destPort, setDestPort] = useState("");
  const [busy, setBusy] = useState(false);

  const listenErr = listen ? validListen(listen) : "";
  const dupErr =
    !busy && listen && forwards.some((f) => f.protocol === proto && f.listen === listen.trim()) ? "该协议下监听端口已被占用" : "";
  const portNum = Number(destPort);
  const portErr = destPort && (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) ? "目标端口范围 1-65535" : "";
  const err = listenErr || dupErr || portErr || (dest ? "" : "请选择目标节点");
  const ready = listen && destPort && !err;

  const commit = async (next: Forward[]) => {
    setBusy(true);
    try {
      await saveForwards(node.id, next);
      return true;
    } catch (e) {
      onError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    if (!ready) return;
    const f: Forward = { nodeId: node.id, protocol: proto, listen: listen.trim(), destNodeId: dest, destPort: portNum };
    if (await commit([...forwards, f])) {
      setListen("");
      setDestPort("");
    }
  };

  return (
    <>
      <p className="tab-intro muted">
        在本节点监听端口，把连接转发到 VPN 内的目标节点。目标节点停用或 IP 冲突时，Master 会暂停下发对应规则。
      </p>
      <div className="fwd-form">
        <select value={proto} onChange={(e) => setProto(e.target.value)} aria-label="协议">
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
        </select>
        <input placeholder="监听端口" value={listen} onChange={(e) => setListen(e.target.value)} aria-invalid={!!listenErr || !!dupErr} />
        <span className="fwd-arrow">→</span>
        <select value={dest} onChange={(e) => setDest(e.target.value)} aria-label="目标节点">
          {targets.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name || shortName(t)} ({hostKey(t.address) || "无地址"})
            </option>
          ))}
        </select>
        <input placeholder="端口" value={destPort} onChange={(e) => setDestPort(e.target.value)} aria-invalid={!!portErr} />
        <button type="button" className="mini" disabled={busy || !ready} onClick={() => void add()}>
          添加
        </button>
      </div>
      <div className={`field-err${(listen || destPort) && err ? " show" : ""}`}>{(listen || destPort) && err ? err : " "}</div>

      {forwards.length === 0 ? (
        <div className="muted empty-note">尚未配置端口转发。</div>
      ) : (
        <ul className="rule-list">
          {forwards.map((f) => {
            const live = liveFor(f, stats?.forwards);
            const target = mesh?.nodes.find((n) => n.id === f.destNodeId);
            const paused = !!target?.disabled;
            return (
              <li key={`${f.protocol} ${f.listen}`} className={`rule-item${paused ? " paused" : ""}`}>
                <span className={`rule-badge ${f.protocol}`}>{f.protocol.toUpperCase()}</span>
                <div className="rule-main">
                  <div>
                    <code>:{f.listen}</code> → {nodeName(f.destNodeId)}
                    <code>:{f.destPort}</code>
                  </div>
                  <div className="tiny muted">
                    {paused ? "目标已停用 · 规则暂停" : live ? `↓ ${formatBytes(live.rx)} · ↑ ${formatBytes(live.tx)}` : "暂无流量"}
                  </div>
                </div>
                <button
                  type="button"
                  className="rule-del"
                  aria-label="删除规则"
                  disabled={busy}
                  onClick={() => void commit(forwards.filter((x) => x !== f))}
                >
                  ✕
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
