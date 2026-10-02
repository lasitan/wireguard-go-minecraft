import { useCallback, useEffect, useState } from "react";
import {
  DeletePasskey,
  ListPasskeys,
  RegisterPasskey,
  RenamePasskey,
  beginPasskeyAssert,
  type PasskeyInfo,
} from "../../api/WebAuthn";
import { passkeySupported } from "../../api/webauthnCodec";
import { state } from "../../core/state";
import { showToast } from "../../ui/toastStore";
import "./passkeyCard.css";

type StepUpMode = "password" | "passkey";

export function PasskeyCard() {
  const [list, setList] = useState<PasskeyInfo[]>([]);
  const [loading, setLoading] = useState(!state.demo);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [stepUp, setStepUp] = useState<null | { kind: "rename" | "delete"; item: PasskeyInfo }>(null);
  const [stepMode, setStepMode] = useState<StepUpMode>("password");
  const [password, setPassword] = useState("");
  const [renameTo, setRenameTo] = useState("");

  const reload = useCallback(async () => {
    if (state.demo) {
      setLoading(false);
      return;
    }
    try {
      setList(await ListPasskeys());
    } catch (ex) {
      showToast((ex as Error).message || "加载通行密钥失败", "error");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  if (state.demo) {
    return (
      <div className="passkey-card">
        <div className="passkey-title">通行密钥</div>
      </div>
    );
  }

  async function onAdd() {
    if (!passkeySupported()) {
      showToast("当前浏览器不支持通行密钥", "error");
      return;
    }
    setBusy(true);
    try {
      await RegisterPasskey(name.trim() || "通行密钥");
      setName("");
      showToast("通行密钥已添加", "success");
      await reload();
    } catch (ex) {
      showToast((ex as Error).message || "添加失败", "error");
    } finally {
      setBusy(false);
    }
  }

  function openStep(kind: "rename" | "delete", item: PasskeyInfo) {
    setStepUp({ kind, item });
    setStepMode("password");
    setPassword("");
    setRenameTo(item.name);
  }

  async function confirmStep() {
    if (!stepUp) return;
    setBusy(true);
    try {
      let auth: { password?: string; webauthnSessionId?: string; webauthnCredential?: Record<string, unknown> };
      if (stepMode === "password") {
        if (!password) {
          showToast("请输入密码", "error");
          setBusy(false);
          return;
        }
        auth = { password };
      } else {
        const assertion = await beginPasskeyAssert();
        auth = { webauthnSessionId: assertion.sessionId, webauthnCredential: assertion.credential };
      }
      if (stepUp.kind === "rename") {
        await RenamePasskey(stepUp.item.id, renameTo.trim() || stepUp.item.name, auth);
        showToast("已改名", "success");
      } else {
        await DeletePasskey(stepUp.item.id, auth);
        showToast("已删除通行密钥", "success");
      }
      setStepUp(null);
      await reload();
    } catch (ex) {
      showToast((ex as Error).message || "操作失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="passkey-card">
      <div className="passkey-head">
        <div className="passkey-title">通行密钥</div>
      </div>
      {loading ? <p className="muted">加载中…</p> : null}
      {!loading && list.length === 0 ? <p className="muted">暂无</p> : null}
      <ul className="passkey-list">
        {list.map((item) => (
          <li key={item.id}>
            <div>
              <b>{item.name}</b>
              <span className="muted tiny">{new Date(item.createdAt).toLocaleString()}</span>
            </div>
            <div className="passkey-actions">
              <button type="button" className="secondary" disabled={busy} onClick={() => openStep("rename", item)}>
                改名
              </button>
              <button type="button" className="secondary danger-btn" disabled={busy} onClick={() => openStep("delete", item)}>
                删除
              </button>
            </div>
          </li>
        ))}
      </ul>
      <div className="passkey-add">
        <input
          type="text"
          placeholder="名称（可选）"
          value={name}
          disabled={busy}
          onChange={(e) => setName(e.target.value)}
        />
        <button type="button" disabled={busy || !passkeySupported()} onClick={() => void onAdd()}>
          添加通行密钥
        </button>
      </div>

      {stepUp ? (
        <div className="passkey-stepup">
          <div className="passkey-stepup-title">
            {stepUp.kind === "rename" ? "改名" : "删除"}「{stepUp.item.name}」
          </div>
          {stepUp.kind === "rename" ? (
            <label>
              新名称
              <input type="text" value={renameTo} disabled={busy} onChange={(e) => setRenameTo(e.target.value)} />
            </label>
          ) : null}
          <div className="passkey-step-modes">
            <button type="button" className={stepMode === "password" ? "active" : "secondary"} disabled={busy} onClick={() => setStepMode("password")}>
              密码验证
            </button>
            <button
              type="button"
              className={stepMode === "passkey" ? "active" : "secondary"}
              disabled={busy || !passkeySupported() || (stepUp.kind === "delete" && list.length < 2)}
              onClick={() => setStepMode("passkey")}
            >
              通行密钥验证
            </button>
          </div>
          {stepMode === "password" ? (
            <label>
              管理员密码
              <input type="password" value={password} disabled={busy} autoFocus onChange={(e) => setPassword(e.target.value)} />
            </label>
          ) : null}
          <div className="passkey-step-actions">
            <button type="button" className="secondary" disabled={busy} onClick={() => setStepUp(null)}>
              取消
            </button>
            <button type="button" disabled={busy} onClick={() => void confirmStep()}>
              确认
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
