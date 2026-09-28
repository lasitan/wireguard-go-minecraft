import { useEffect, useRef, useState } from "react";
import type { UpdateCommands } from "../../core/models";

type CmdTab = { key: keyof UpdateCommands; label: string; hint: string };

const UPDATE_TABS: CmdTab[] = [
  { key: "installed", label: "已安装", hint: "在已安装的 Master / Agent 上执行，自动下载并重启服务" },
  { key: "linux", label: "Linux", hint: "全新安装或升级；有 dpkg 时安装 .deb，否则安装二进制" },
  { key: "linuxCn", label: "国内镜像", hint: "通过 ghfast.top 镜像下载，适合无法直连 GitHub 的机器" },
  { key: "windows", label: "Windows", hint: "在「以管理员身份运行」的 PowerShell 中执行" },
];

export function UpdateCommandBox({
  commands,
  tabs = UPDATE_TABS,
  defaultTab = 0,
}: {
  commands: UpdateCommands;
  tabs?: CmdTab[];
  defaultTab?: number;
}) {
  const [tab, setTab] = useState(defaultTab);
  const [copied, setCopied] = useState(false);
  const resetRef = useRef(0);
  const cur = tabs[tab] ?? tabs[0];
  const cmd = commands[cur.key];

  // Callers may pass a fresh tabs array every render; reset only when the set changes.
  const tabsKey = tabs.map((t) => t.key).join(",");
  useEffect(() => {
    setTab(defaultTab);
  }, [defaultTab, tabsKey]);

  useEffect(() => () => window.clearTimeout(resetRef.current), []);

  const copy = async () => {
    await navigator.clipboard.writeText(cmd);
    setCopied(true);
    window.clearTimeout(resetRef.current);
    resetRef.current = window.setTimeout(() => setCopied(false), 1600);
  };

  return (
    <div className="cmd-box">
      <div className="cmd-tabs" style={{ ["--seg-count" as string]: tabs.length }} role="tablist">
        <span className="cmd-ink" style={{ transform: `translateX(${tab * 100}%)` }} aria-hidden />
        {tabs.map((t, i) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            aria-selected={i === tab}
            className={i === tab ? "active" : ""}
            onClick={() => {
              setTab(i);
              setCopied(false);
            }}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="cmd-line" key={cur.key}>
        <code>{cmd}</code>
        <button type="button" className={`cmd-copy${copied ? " done" : ""}`} onClick={() => void copy()}>
          {copied ? "已复制" : "复制"}
        </button>
      </div>
      <p className="tiny muted cmd-hint" key={cur.key + "-hint"}>
        {cur.hint}
      </p>
    </div>
  );
}
