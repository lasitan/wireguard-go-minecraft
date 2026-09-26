import { useState, type FormEvent } from "react";
import { login } from "../../app/Session";
import { state } from "../../core/state";
import "./login.css";

export function LoginPage() {
  const [err, setErr] = useState(state.err);

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const password = String(new FormData(e.currentTarget).get("password") || "");
    try {
      await login(password);
    } catch (ex) {
      const msg = (ex as Error).message;
      state.err = msg;
      setErr(msg);
    }
  }

  return (
    <main className="login-wrap">
      <div className="login-card">
        <h2>Master 控制台</h2>
        <p className="muted">使用 wireguard-go-master.json 中的 adminPassword</p>
        <form onSubmit={onSubmit}>
          <label>密码</label>
          <input type="password" name="password" required autoFocus />
          <button type="submit">登录</button>
        </form>
        {err ? <p className="error">{err}</p> : null}
      </div>
    </main>
  );
}
