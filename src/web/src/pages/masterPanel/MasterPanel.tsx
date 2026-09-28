import { state } from "../../core/state";
import { goHome } from "../../app/FocusNav";
import { patchMeta } from "../../app/MetaActions";
import { logout } from "../../app/Session";
import { randomEnrollToken, validateEnrollToken, validatePool } from "../../utils/validateSettings";
import { SettingField } from "./SettingField";
import { BootstrapInstallCard } from "./BootstrapInstallCard";
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
        <BootstrapInstallCard />
        <div className="detail">
          <span>Listen</span>
          <b>{state.meta?.listen || "—"}</b>
        </div>
        <div className="detail">
          <span>默认网卡</span>
          <b>{state.meta?.defaultIface || "wg0"}</b>
        </div>
        <div className="detail">
          <span>轮询</span>
          <b>{state.meta?.defaultPoll || "10s"}</b>
        </div>
        <SettingField
          label="入网密钥"
          hint="填入 Agent 配置的 key 字段即可入网。修改后旧密钥立即失效，已入网节点不受影响。"
          value={state.meta?.enrollToken || ""}
          validate={validateEnrollToken}
          onSave={(v) => patchMeta({ enrollToken: v })}
          generate={randomEnrollToken}
          copyable
          mono
        />
        <SettingField
          label="新节点地址池"
          hint="Agent 入网时从此网段自动分配 VPN 地址；修改只影响之后入网的节点。"
          value={state.meta?.vpnSubnet || ""}
          validate={validatePool}
          onSave={(v) => patchMeta({ vpnSubnet: v })}
          mono
        />
        {!state.demo ? (
          <button type="button" className="secondary" style={{ marginTop: "0.75rem" }} onClick={() => logout()}>
            退出登录
          </button>
        ) : null}
      </div>
    </>
  );
}
