import { useEffect, useState } from "react";
import type { AgentInstallCommands } from "../../core/models";
import { demoAgentInstallCommands } from "../../core/demoBootstrap";
import { state } from "../../core/state";
import { GetAgentInstallCommands } from "../../api/GetAgentInstallCommands";
import { UpdateCommandBox } from "./UpdateCommandBox";

const INSTALL_TABS = [
  { key: "linux" as const, label: "Linux" },
  { key: "linuxCn" as const, label: "国内镜像" },
  { key: "windows" as const, label: "Windows" },
];

export function BootstrapInstallCard() {
  const [asServer, setAsServer] = useState(false);
  const [endpoint, setEndpoint] = useState("");
  const [cmds, setCmds] = useState<AgentInstallCommands | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    let cancelled = false;
    const role = asServer ? "server" : "client";
    const load = async () => {
      setErr("");
      try {
        const c = state.demo ? demoAgentInstallCommands(role, endpoint) : await GetAgentInstallCommands(role, endpoint);
        if (!cancelled) setCmds(c);
      } catch (e) {
        if (!cancelled) {
          setCmds(null);
          setErr((e as Error).message);
        }
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [asServer, endpoint]);

  const box = cmds
    ? {
        installed: cmds.linux,
        linux: cmds.linux,
        linuxCn: cmds.linuxCn,
        windows: cmds.windows,
      }
    : null;

  return (
    <section className="bootstrap-card">
      <div className="bootstrap-head">
        <div>
          <div className="update-kicker">新机器</div>
          <b className="bootstrap-title">一键安装 Agent</b>
        </div>
      </div>

      <label className="bootstrap-check">
        <input type="checkbox" checked={asServer} onChange={(e) => setAsServer(e.target.checked)} />
        <span>磁吸母卡</span>
      </label>

      <div className={`bootstrap-endpoint${asServer ? " show" : ""}`}>
        <label className="bootstrap-ep-label">
          <span className="tiny muted">公网 dial 地址</span>
          <input
            type="text"
            placeholder="host:port"
            value={endpoint}
            disabled={!asServer}
            onChange={(e) => setEndpoint(e.target.value)}
          />
        </label>
      </div>

      {box ? (
        <UpdateCommandBox
          commands={box}
          tabs={INSTALL_TABS}
          defaultTab={0}
        />
      ) : (
        <div className="update-card loading muted">{err || "正在生成命令…"}</div>
      )}
      <div className={`field-err${err ? " show" : ""}`}>{err || " "}</div>
    </section>
  );
}
