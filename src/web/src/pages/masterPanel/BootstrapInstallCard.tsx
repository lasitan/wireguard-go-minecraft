import { useEffect, useState } from "react";
import type { AgentInstallCommands } from "../../core/models";
import { demoAgentInstallCommands } from "../../core/demoBootstrap";
import { state } from "../../core/state";
import { GetAgentInstallCommands } from "../../api/GetAgentInstallCommands";
import { UpdateCommandBox } from "./UpdateCommandBox";

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
      <p className="tiny muted bootstrap-lead">
        在<strong>尚未安装</strong> wireguard-mc 的 Linux / Windows 上以管理员执行下方整行命令：自动下载、写入配置、注册开机自启并连接本 Master（
        {cmds?.masterUrl || "…"}）。
      </p>

      <label className="bootstrap-check">
        <input type="checkbox" checked={asServer} onChange={(e) => setAsServer(e.target.checked)} />
        <span>作为磁吸母卡 / 服务端（监听 TCP 25590，其他节点可连本机）</span>
      </label>

      <div className={`bootstrap-endpoint${asServer ? " show" : ""}`}>
        <label className="bootstrap-ep-label">
          <span className="tiny muted">公网 dial 地址（可选，留空则入网时用本机公网 IP）</span>
          <input
            type="text"
            placeholder="例如 203.0.113.10:25590"
            value={endpoint}
            disabled={!asServer}
            onChange={(e) => setEndpoint(e.target.value)}
          />
        </label>
      </div>

      {box ? (
        <UpdateCommandBox
          commands={box}
          tabs={[
            { key: "linux", label: "Linux", hint: "需要 curl 与 sudo；有 dpkg 时装 .deb，否则装二进制" },
            { key: "linuxCn", label: "国内镜像", hint: "经 ghfast.top 下载，适合无法直连 GitHub 的机器" },
            { key: "windows", label: "Windows", hint: "以管理员身份打开 PowerShell，粘贴整行执行" },
          ]}
          defaultTab={0}
        />
      ) : (
        <div className="update-card loading muted">{err || "正在生成命令…"}</div>
      )}
      <div className={`field-err${err ? " show" : ""}`}>{err || " "}</div>
    </section>
  );
}
