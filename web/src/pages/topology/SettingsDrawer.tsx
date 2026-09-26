import { MASTER_ID } from "../../core/constants";
import { notify, state } from "../../core/state";
import { hostOf } from "../../utils/hostOf";
import { isOnline } from "../../utils/isOnline";
import { shortName } from "../../utils/shortName";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { goHome } from "../../app/FocusNav";
import { logout, removeNode } from "../../app/Session";
import "./drawer.css";

export function SettingsDrawer() {
  const open = state.drawerOpen;
  const swap = state.contentSwap;
  const id = state.selectedId;

  return (
    <>
      <div
        className={`drawer-scrim${open ? " show" : ""}`}
        id="drawer-scrim"
        onClick={() => void goHome()}
      />
      <aside
        id="settings-drawer"
        className={`settings-drawer${open ? " open" : ""}${swap ? " content-swap" : ""}`}
      >
        <DrawerBody id={id} />
      </aside>
    </>
  );
}

function DrawerBody({ id }: { id: string | null }) {
  if (!id) {
    return <div className="drawer-body muted" style={{ paddingTop: "2rem" }}>选择 Master 或 Agent</div>;
  }

  if (id === MASTER_ID) {
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

  const n = (state.mesh?.nodes || []).find((x) => x.id === id);
  if (!n) return null;
  const conflicts = state.mesh ? ResolveIpConflicts(state.mesh) : new Map();
  const conflict = conflicts.has(n.id);

  return (
    <>
      <div className="drawer-head">
        <div>
          <div className="drawer-kicker">
            {n.role}
            {conflict ? " · 冲突" : ""}
          </div>
          <h2>{n.name || shortName(n)}</h2>
        </div>
        <button type="button" className="icon-btn" aria-label="关闭" onClick={() => void goHome()}>
          ✕
        </button>
      </div>
      <div className="drawer-body">
        <div className="detail">
          <span>状态</span>
          <b className={conflict ? "warn" : isOnline(n) ? "ok" : "muted"}>
            {conflict ? "IP 冲突" : isOnline(n) ? "在线" : "离线"}
          </b>
        </div>
        <div className="detail">
          <span>地址</span>
          <b>{n.address || "—"}</b>
        </div>
        <div className="detail">
          <span>Endpoint</span>
          <b>{n.endpoint || "—"}</b>
        </div>
        <div className="detail">
          <span>UUID</span>
          <code className="tiny">{n.id}</code>
        </div>
        <div className="detail">
          <span>Token</span>
          <code className="tiny">{n.token || ""}</code>
        </div>
        {conflict ? (
          <p className="warn-note">
            与另一节点共用 {hostOf(n.address)}。前端仅标黄展示互斥，不影响已在运行的隧道。
          </p>
        ) : null}
        <button
          type="button"
          className="danger"
          onClick={() => {
            removeNode(n.id).catch((e) => {
              state.err = e.message;
              notify();
            });
          }}
        >
          删除节点
        </button>
      </div>
    </>
  );
}
