import { useState, type FormEvent } from "react";
import { login, loginWithPasskey } from "../../app/Session";
import { passkeySupported } from "../../api/webauthnCodec";
import { ToastHost } from "../../ui/ToastHost";
import { showToast } from "../../ui/toastStore";
import "./login.css";

export function LoginPage() {
  const [busy, setBusy] = useState(false);
  const canPasskey = passkeySupported();

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const password = String(new FormData(e.currentTarget).get("password") || "");
    setBusy(true);
    try {
      await login(password);
    } catch (ex) {
      showToast((ex as Error).message || "登录失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function onPasskey() {
    setBusy(true);
    try {
      await loginWithPasskey();
    } catch (ex) {
      showToast((ex as Error).message || "通行密钥登录失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-wrap">
      <ToastHost />
      <div className="login-card">
        <h2>Master 控制台</h2>
        <p className="muted">使用 lasitan-cluster-master.json 中的 adminPassword，或已注册的通行密钥</p>
        <form onSubmit={onSubmit}>
          <label>密码</label>
          <input type="password" name="password" required autoFocus disabled={busy} />
          <button type="submit" disabled={busy}>
            登录
          </button>
        </form>
        {canPasskey ? (
          <button type="button" className="secondary passkey-btn" disabled={busy} onClick={() => void onPasskey()}>
            使用通行密钥
          </button>
        ) : null}
      </div>
    </main>
  );
}
