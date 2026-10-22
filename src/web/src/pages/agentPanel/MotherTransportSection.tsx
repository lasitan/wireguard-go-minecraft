import { useEffect, useMemo, useState } from "react";
import type { Node } from "../../core/models";
import { patchNode } from "../../app/NodeActions";
import {
  CAMO_PROFILES,
  buildTransportJson,
  parseTransport,
  type CamoProfile,
  type TransportCamouflage,
} from "../../core/transportConfig";
import "../masterPanel/transportCard.css";

function validateCamo(c: TransportCamouflage): string {
  if (c.profile !== "none" && c.loginPluginSecret.trim().length < 8) {
    return "共享密钥至少 8 个字符（连接此母卡的节点必须一致）";
  }
  if (c.profile === "minecraft" && !c.loginPluginChannel.trim()) {
    return "登录插件 Channel 不能为空";
  }
  return "";
}

/** Mother transport is exclusive: TCP camouflage XOR native WireGuard UDP. */
export function MotherTransportSection({
  node,
  busy,
  run,
}: {
  node: Node;
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
}) {
  const serverKey = JSON.stringify(node.transport ?? null);
  const server = useMemo(() => parseTransport(node.transport).camouflage, [serverKey]);
  const [camo, setCamo] = useState(server);
  const [synced, setSynced] = useState(server);
  const dirty = JSON.stringify(camo) !== JSON.stringify(synced);
  useEffect(() => {
    setSynced(server);
    setCamo((cur) => (JSON.stringify(cur) === JSON.stringify(synced) ? server : cur));
  }, [server]);
  const [msg, setMsg] = useState("");
  const err = validateCamo(camo);
  const udp = !!node.udpMode;

  async function setMode(nextUdp: boolean) {
    setMsg("");
    if (nextUdp === udp) return;
    try {
      await run(() => patchNode(node.id, { udpMode: nextUdp }));
      setMsg(nextUdp ? "已切换为原生 UDP" : "已切换为 TCP 伪装");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "切换失败");
    }
  }

  async function saveCamo(next: TransportCamouflage) {
    setMsg("");
    try {
      const transportJson = buildTransportJson(next, node.transport);
      const body = JSON.parse(transportJson) as unknown;
      // Saving camouflage always selects TCP mode (exclusive with UDP).
      await run(() => patchNode(node.id, { udpMode: false, transport: body }));
      const saved = parseTransport(body).camouflage;
      setSynced(saved);
      setCamo(saved);
      setMsg("已保存");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "保存失败");
    }
  }

  return (
    <section className="transport-card" style={{ marginTop: "0.75rem" }}>
      <h3 className="transport-title">传输模式</h3>
      <p className="transport-hint">TCP 伪装与原生 UDP 只能二选一；UDP 不做伪装，走原生 WireGuard 以拉满性能。</p>
      <div className="transport-mode" role="radiogroup" aria-label="传输模式">
        <label className={`transport-mode-opt${udp ? "" : " active"}`}>
          <input
            type="radio"
            name={`bind-mode-${node.id}`}
            checked={!udp}
            disabled={busy}
            onChange={() => void setMode(false)}
          />
          <span>
            <b>TCP 伪装</b>
            <small>游戏协议外壳，便于穿透限制</small>
          </span>
        </label>
        <label className={`transport-mode-opt${udp ? " active" : ""}`}>
          <input
            type="radio"
            name={`bind-mode-${node.id}`}
            checked={udp}
            disabled={busy}
            onChange={() => void setMode(true)}
          />
          <span>
            <b>UDP 原生</b>
            <small>标准 WireGuard，无伪装，性能最高</small>
          </span>
        </label>
      </div>

      {udp ? (
        <p className="transport-msg">当前为原生 WireGuard UDP：不加载伪装配置，吸附节点同样走 UDP。</p>
      ) : (
        <>
          <label className="transport-field">
            <span>伪装协议</span>
            <select
              value={camo.profile}
              disabled={busy}
              onChange={(e) => setCamo({ ...camo, profile: e.target.value as CamoProfile })}
            >
              {CAMO_PROFILES.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
          </label>
          {camo.profile !== "none" ? (
            <>
              <label className="transport-check">
                <input
                  type="checkbox"
                  checked={camo.deep}
                  disabled={busy}
                  onChange={(e) => setCamo({ ...camo, deep: e.target.checked })}
                />
                深度伪装
              </label>
              <label className="transport-check">
                <input
                  type="checkbox"
                  checked={camo.secure}
                  disabled={busy}
                  onChange={(e) => setCamo({ ...camo, secure: e.target.checked })}
                />
                防中间人加密
              </label>
              <label className="transport-field">
                <span>共享密钥</span>
                <input
                  type="password"
                  value={camo.loginPluginSecret}
                  disabled={busy}
                  autoComplete="off"
                  onChange={(e) => setCamo({ ...camo, loginPluginSecret: e.target.value })}
                />
              </label>
              {camo.profile === "minecraft" ? (
                <>
                  <label className="transport-field">
                    <span>MC 用户名</span>
                    <input
                      value={camo.loginUsername}
                      disabled={busy}
                      onChange={(e) => setCamo({ ...camo, loginUsername: e.target.value })}
                    />
                  </label>
                  <label className="transport-field">
                    <span>登录插件 Channel</span>
                    <input
                      value={camo.loginPluginChannel}
                      disabled={busy}
                      onChange={(e) => setCamo({ ...camo, loginPluginChannel: e.target.value })}
                    />
                  </label>
                </>
              ) : (
                <label className="transport-field">
                  <span>服务器显示名</span>
                  <input
                    value={camo.serverName}
                    disabled={busy}
                    onChange={(e) => setCamo({ ...camo, serverName: e.target.value })}
                  />
                </label>
              )}
            </>
          ) : null}
          {err ? <p className="transport-err">{err}</p> : null}
          {msg && !dirty ? <p className="transport-msg">{msg}</p> : null}
          {dirty && !err ? <p className="transport-msg">有未保存的修改</p> : null}
          <button type="button" className="primary" disabled={busy || !!err} onClick={() => void saveCamo(camo)}>
            {busy ? "保存中…" : "保存伪装配置"}
          </button>
        </>
      )}
      {udp && msg ? <p className="transport-msg">{msg}</p> : null}
    </section>
  );
}
