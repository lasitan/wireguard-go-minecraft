import { state } from "../../core/state";
import { goHome } from "../../app/FocusNav";
import { logout } from "../../app/Session";
import { UpdateCard } from "./UpdateCard";
import "./masterPanel.css";

export function MasterPanel() {
  return (
    <>
      <div className="drawer-head">
        <div>
          <div className="drawer-kicker">Control plane</div>
          <h2>Master</h2>
        </div>
        <button type="button" className="icon-btn" aria-label="关闭" onClick={() => void goHome()}>
          ✕
        </button>
      </div>
      <div className="drawer-body">
        <UpdateCard />
        <div className="detail">
          <span>Listen</span>
          <b>{state.meta?.listen || "—"}</b>
        </div>
        <div className="detail">
          <span>网段</span>
          <b>{state.meta?.vpnSubnet || "—"}</b>
        </div>
        <div className="detail">
          <span>默认网卡</span>
          <b>{state.meta?.defaultIface || "wg0"}</b>
        </div>
        <div className="detail">
          <span>轮询</span>
          <b>{state.meta?.defaultPoll || "10s"}</b>
        </div>
        <label>enrollToken（Agent 入网密钥）</label>
        <code className="token-box">{state.meta?.enrollToken || ""}</code>
        <button
          type="button"
          className="secondary"
          onClick={() => void navigator.clipboard.writeText(state.meta?.enrollToken || "")}
        >
          复制 enrollToken
        </button>
        {!state.demo ? (
          <button type="button" className="secondary" style={{ marginTop: "0.75rem" }} onClick={() => logout()}>
            退出登录
          </button>
        ) : null}
      </div>
    </>
  );
}
