import { useMemo, useState } from "react";
import type { Forward, LiveForward, Node, NodeStats } from "../../core/models";
import { state } from "../../core/state";
import { saveForwards } from "../../app/NodeActions";
import { formatBytes } from "../../utils/formatBytes";
import { hostKey } from "../../utils/hostOf";
import { isIPv4 } from "../../utils/validateCidr";
import { shortName } from "../../utils/shortName";

type Span = { start: number; end: number };

function parseSpan(s: string): Span | null {
  const v = s.trim();
  if (v === "*") return { start: 1, end: 65535 };
  const range = /^(\d+)-(\d+)$/.exec(v);
  if (range) {
    const start = Number(range[1]);
    const end = Number(range[2]);
    if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > 65535 || start > end) return null;
    return { start, end };
  }
  const n = Number(v);
  if (!Number.isInteger(n) || n < 1 || n > 65535) return null;
  return { start: n, end: n };
}

function parseListen(s: string): { host: string; span: Span } | null {
  const v = s.trim();
  let host = "0.0.0.0";
  let port = v;
  const i = v.lastIndexOf(":");
  if (i >= 0) {
    host = v.slice(0, i).replace(/^\[|\]$/g, "");
    port = v.slice(i + 1);
    if (!isIPv4(host)) return null;
  }
  const span = parseSpan(port);
  if (!span) return null;
  return { host, span };
}

function validListen(s: string): string {
  if (!s.trim()) return "请填写监听端口";
  if (!parseListen(s)) return "端口、范围 1-65535，或 *";
  return "";
}

function resolveDest(listen: Span, dest: string): Span | null {
  const v = dest.trim();
  if (v === "*") return listen;
  const span = parseSpan(v);
  if (!span) return null;
  if (span.start === span.end && listen.end > listen.start) {
    const end = span.start + (listen.end - listen.start);
    if (end > 65535) return null;
    return { start: span.start, end };
  }
  if (span.end - span.start !== listen.end - listen.start) return null;
  return span;
}

function protoBadge(p: string): string {
  if (p === "tcp/udp") return "TCP/UDP";
  return p.toUpperCase();
}

function protoOverlap(a: string, b: string): boolean {
  if (a === b) return true;
  if (a === "tcp/udp" && (b === "tcp" || b === "udp" || b === "tcp/udp")) return true;
  if (b === "tcp/udp" && (a === "tcp" || a === "udp")) return true;
  return false;
}

function coversListen(existing: Forward, proto: string, listen: string): boolean {
  if (!protoOverlap(existing.protocol, proto)) return false;
  const a = parseListen(existing.listen);
  const b = parseListen(listen);
  if (!a || !b) return existing.listen === listen;
  if (a.span.end < b.span.start || b.span.end < a.span.start) return false;
  return a.host === "0.0.0.0" || b.host === "0.0.0.0" || a.host === b.host;
}

function destLabel(f: Forward): string {
  return f.dest || String(f.destPort);
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
  const parsedListen = listenTrim ? parseListen(listenTrim) : null;
  const destTrim = destPort.trim();
  const destSpan = parsedListen && destTrim ? resolveDest(parsedListen.span, destTrim) : null;
  const dupErr =
    !busy && listenTrim && forwards.some((f) => coversListen(f, proto, listenTrim)) ? "监听端口已被占用" : "";
  const portErr = destTrim && parsedListen && !destSpan ? "目标端口、等长范围，或 *" : "";
  const err = listenErr || dupErr || portErr || (dest ? "" : "请选择目标节点");
  const ready = !!parsedListen && !!destSpan && !err;

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
    if (!parsedListen || !destSpan) return;
    const destCanon = destTrim === "*" ? "*" : destSpan.start === destSpan.end ? String(destSpan.start) : destSpan.start === 1 && destSpan.end === 65535 ? "*" : `${destSpan.start}-${destSpan.end}`;
    const destNum = destCanon === "*" ? 0 : destSpan.start;
    const sameRule = (f: Forward) => f.listen === listenTrim && f.destNodeId === dest && destLabel(f) === destCanon;
    let next = [...forwards];
    // Promote a matching single-protocol sibling to tcp/udp when adding the other half or dual.
    if (proto === "tcp/udp") {
      next = next.filter((f) => !(sameRule(f) && (f.protocol === "tcp" || f.protocol === "udp")));
    } else {
      const sibling = next.find(
        (f) => sameRule(f) && ((proto === "tcp" && f.protocol === "udp") || (proto === "udp" && f.protocol === "tcp")),
      );
      if (sibling) {
        next = next.filter((f) => f !== sibling);
        next.push({ nodeId: node.id, protocol: "tcp/udp", listen: listenTrim, destNodeId: dest, dest: destCanon, destPort: destNum });
        if (await commit(next)) {
          setListen("");
          setDestPort("");
        }
        return;
      }
    }
    const f: Forward = { nodeId: node.id, protocol: proto, listen: listenTrim, destNodeId: dest, dest: destCanon, destPort: destNum };
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
        <input placeholder="端口、范围或 *" value={listen} onChange={(e) => setListen(e.target.value)} aria-invalid={!!listenErr || !!dupErr} />
        <span className="fwd-arrow">→</span>
        <select value={dest} onChange={(e) => setDest(e.target.value)} aria-label="目标节点">
          {targets.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name || shortName(t)} ({hostKey(t.address) || "无地址"})
            </option>
          ))}
        </select>
        <input placeholder="端口、范围或 *" value={destPort} onChange={(e) => setDestPort(e.target.value)} aria-invalid={!!portErr} />
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
              <li key={`${f.protocol} ${f.listen} ${f.destNodeId} ${destLabel(f)}`} className={`rule-item${paused ? " paused" : ""}`}>
                <span className={`rule-badge ${f.protocol === "tcp/udp" ? "both" : f.protocol}`}>{protoBadge(f.protocol)}</span>
                <div className="rule-main">
                  <div>
                    <code>:{f.listen}</code> → {nodeName(f.destNodeId)}
                    <code>:{destLabel(f)}</code>
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
