import { state } from "../../core/state";
import { goHome } from "../../app/FocusNav";
import { patchMeta } from "../../app/MetaActions";
import { logout } from "../../app/Session";
import { randomEnrollToken, validateEnrollToken, validatePool, validateRelayPort } from "../../utils/validateSettings";
import { SettingField } from "./SettingField";
import { BootstrapInstallCard } from "./BootstrapInstallCard";
import { TransportCard } from "./TransportCard";
import { UpdateCard } from "./UpdateCard";
import { PasskeyCard } from "./PasskeyCard";
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
        <TransportCard />
        <PasskeyCard />
        <div className="detail">
          <span>Listen</span>
          <b>{state.meta?.listen || "—"}</b>
        </div>
        <div className="detail">
          <span>默认网卡</span>
          <b>{state.meta?.defaultIface || "lc0"}</b>
        </div>
        <div className="detail">
          <span>轮询</span>
          <b>{state.meta?.defaultPoll || "10s"}</b>
        </div>
        <SettingField
          label="入网密钥"
          value={state.meta?.enrollToken || ""}
          validate={validateEnrollToken}
          onSave={(v) => patchMeta({ enrollToken: v })}
          generate={randomEnrollToken}
          copyable
          mono
        />
        <SettingField
          label="新节点地址池"
          value={state.meta?.vpnSubnet || ""}
          validate={validatePool}
          onSave={(v) => patchMeta({ vpnSubnet: v })}
          mono
        />
        <SettingField
          label="跨网段兜底中转端口"
          value={String(state.meta?.relayPort ?? 0)}
          validate={validateRelayPort}
          onSave={(v) => patchMeta({ relayPort: Number(v) })}
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
