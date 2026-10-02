import { useEffect, useRef, useState } from "react";
import type { UpdateCommands } from "../../core/models";

type CmdTab = { key: keyof UpdateCommands; label: string };

const UPDATE_TABS: CmdTab[] = [
  { key: "installed", label: "已安装" },
  { key: "linux", label: "Linux" },
  { key: "linuxCn", label: "国内镜像" },
  { key: "windows", label: "Windows" },
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
    </div>
  );
}
