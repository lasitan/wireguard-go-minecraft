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

function protoBadge(p: string): string {
  if (p === "tcp/udp") return "TCP/UDP";
  return p.toUpperCase();
}

function coversListen(existing: Forward, proto: string, listen: string): boolean {
  if (existing.listen !== listen) return false;
  if (existing.protocol === proto) return true;
  if (existing.protocol === "tcp/udp" && (proto === "tcp" || proto === "udp" || proto === "tcp/udp")) return true;
  if (proto === "tcp/udp" && (existing.protocol === "tcp" || existing.protocol === "udp")) return true;
  return false;
}

function liveFor(f: Forward, live: LiveForward[] | undefined): LiveForward | undefined {
  if (f.protocol === "tcp/udp") {
    return live?.find((l) => (l.protocol === "tcp" || l.protocol === "udp") && l.listen === f.listen);
  }
  return live?.find((l) => l.protocol === f.protocol && l.listen === f.listen);
}

function liveBytes(f: Forward, live: LiveForward[] | undefined): { rx: number; tx: number } | null {
  if (!live) return null;
  if (f.protocol !== "tcp/udp") {
    const one = liveFor(f, live);
    return one ? { rx: one.rx, tx: one.tx } : null;
  }
  const parts = live.filter((l) => (l.protocol === "tcp" || l.protocol === "udp") && l.listen === f.listen);
  if (!parts.length) return null;
  return {
    rx: parts.reduce((s, p) => s + p.rx, 0),
    tx: parts.reduce((s, p) => s + p.tx, 0),
  };
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
  const listenTrim = listen.trim();
  const dupErr =
    !busy && listenTrim && forwards.some((f) => coversListen(f, proto, listenTrim)) ? "监听端口已被占用" : "";
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
    let next = [...forwards];
    // Promote a matching single-protocol sibling to tcp/udp when adding the other half or dual.
    if (proto === "tcp/udp") {
      next = next.filter(
        (f) => !(f.listen === listenTrim && f.destNodeId === dest && f.destPort === portNum && (f.protocol === "tcp" || f.protocol === "udp")),
      );
    } else {
      const sibling = next.find(
        (f) =>
          f.listen === listenTrim &&
          f.destNodeId === dest &&
          f.destPort === portNum &&
          ((proto === "tcp" && f.protocol === "udp") || (proto === "udp" && f.protocol === "tcp")),
      );
      if (sibling) {
        next = next.filter((f) => f !== sibling);
        next.push({ nodeId: node.id, protocol: "tcp/udp", listen: listenTrim, destNodeId: dest, destPort: portNum });
        if (await commit(next)) {
          setListen("");
          setDestPort("");
        }
        return;
      }
    }
    const f: Forward = { nodeId: node.id, protocol: proto, listen: listenTrim, destNodeId: dest, destPort: portNum };
    if (await commit([...next, f])) {
      setListen("");
      setDestPort("");
    }
  };

  return (
    <>
      <div className="fwd-form">
        <select value={proto} onChange={(e) => setProto(e.target.value)} aria-label="协议">
          <option value="tcp">TCP</option>
          <option value="udp">UDP</option>
          <option value="tcp/udp">TCP/UDP</option>
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
        <div className="muted empty-note">暂无</div>
      ) : (
        <ul className="rule-list">
          {forwards.map((f) => {
            const bytes = liveBytes(f, stats?.forwards);
            const target = mesh?.nodes.find((n) => n.id === f.destNodeId);
            const paused = !!target?.disabled;
            return (
              <li key={`${f.protocol} ${f.listen} ${f.destNodeId} ${f.destPort}`} className={`rule-item${paused ? " paused" : ""}`}>
                <span className={`rule-badge ${f.protocol === "tcp/udp" ? "both" : f.protocol}`}>{protoBadge(f.protocol)}</span>
                <div className="rule-main">
                  <div>
                    <code>:{f.listen}</code> → {nodeName(f.destNodeId)}
                    <code>:{f.destPort}</code>
                  </div>
                  <div className="tiny muted">
                    {paused ? "已暂停" : bytes ? `↓ ${formatBytes(bytes.rx)} · ↑ ${formatBytes(bytes.tx)}` : "—"}
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
