import { useEffect, useMemo, useState } from "react";
import { patchMeta } from "../../app/MetaActions";
import { state } from "../../core/state";
import "./transportCard.css";
import {
  CAMO_PROFILES,
  buildTransportJson,
  parseTransport,
  type CamoProfile,
  type TransportCamouflage,
} from "../../core/transportConfig";

function validateCamo(c: TransportCamouflage): string {
  if (c.profile !== "none" && c.loginPluginSecret.trim().length < 8) {
    return "共享密钥至少 8 个字符（所有节点必须一致）";
  }
  if (c.profile === "minecraft" && !c.loginPluginChannel.trim()) {
    return "登录插件 Channel 不能为空";
  }
  return "";
}

export function TransportCard() {
  // The 8s poll hands us a fresh meta object each time; only its content matters.
  const serverKey = JSON.stringify(state.meta?.transport ?? null);
  const server = useMemo(() => parseTransport(state.meta?.transport).camouflage, [serverKey]);
  const [camo, setCamo] = useState(server);
  const [synced, setSynced] = useState(server);
  const dirty = JSON.stringify(camo) !== JSON.stringify(synced);
  useEffect(() => {
    setSynced(server);
    // Keep unsaved edits; adopt the server value only when nothing is pending.
    setCamo((cur) => (JSON.stringify(cur) === JSON.stringify(synced) ? server : cur));
  }, [server]);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const err = validateCamo(camo);

  async function save(next: TransportCamouflage) {
    setBusy(true);
    setMsg("");
    try {
      const transportJson = buildTransportJson(next, state.meta?.transport);
      await patchMeta({ transportJson });
      const saved = parseTransport(state.meta?.transport).camouflage;
      setSynced(saved);
      setCamo(saved);
      setMsg("已保存");
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="transport-card">
      <h3 className="transport-title">TCP 传输伪装</h3>
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
      <button
        type="button"
        className="primary"
        disabled={busy || !!err}
        onClick={() => void save(camo)}
      >
        {busy ? "保存中…" : "保存伪装配置"}
      </button>
    </section>
  );
}
