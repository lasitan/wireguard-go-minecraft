import { GetNodeStats } from "../api/GetNodeStats";
import { GetVersion } from "../api/GetVersion";
import { UpgradeTargets } from "../api/UpgradeTargets";
import { demoUpgrade } from "../core/demoVersion";
import type { UpgradeRequest, UpgradeResult, UpgradeStatus } from "../core/models";
import { notify, state } from "../core/state";
import { loadVersion } from "./VersionCheck";

export const MASTER_KEY = "master";

const WATCH_MS = 4000;
const GIVE_UP_MS = 6 * 60_000;
const RELOAD_DELAY_MS = 1800;
let watchTimer: number | null = null;

export function isUpgrading(key: string): boolean {
  const p = state.upgrades[key]?.phase;
  return p === "sending" || p === "running";
}

function setStatus(key: string, patch: Partial<UpgradeStatus>) {
  const base: UpgradeStatus = state.upgrades[key] ?? { phase: "sending", message: "", from: "", at: Date.now() };
  state.upgrades = { ...state.upgrades, [key]: { ...base, ...patch } };
  notify();
}

function versionOf(key: string): string {
  if (key === MASTER_KEY) return state.version?.current || "";
  return state.version?.outdatedAgents.find((a) => a.nodeId === key)?.version || "";
}

function targetKeys(req: UpgradeRequest): string[] {
  const keys = new Set<string>(req.nodeIds || []);
  if (req.outdated) (state.version?.outdatedAgents || []).forEach((a) => keys.add(a.nodeId));
  if (req.master) keys.add(MASTER_KEY);
  return [...keys].filter((k) => !isUpgrading(k));
}

function applyResult(key: string, r: UpgradeResult | undefined) {
  if (!r) return setStatus(key, { phase: "failed", message: "Master 未返回结果" });
  setStatus(key, r.ok ? { phase: "running", message: r.message || "升级进程已启动" } : { phase: "failed", message: r.message });
}

/** Asks the Master to start updaters, then watches until the new versions report in. */
export async function startUpgrade(req: UpgradeRequest, knownVersions: Record<string, string> = {}) {
  const keys = targetKeys(req);
  if (!keys.length) return;
  const now = Date.now();
  keys.forEach((k) =>
    setStatus(k, { phase: "sending", message: "正在下发升级指令…", from: knownVersions[k] ?? versionOf(k), at: now }),
  );

  if (state.demo) {
    await new Promise((r) => setTimeout(r, 700));
    keys.forEach((k) => setStatus(k, { phase: "running", message: "升级进程已启动（演示）" }));
    window.setTimeout(() => {
      keys.forEach((k) => {
        demoUpgrade(k);
        setStatus(k, { phase: "done", message: "已升级到最新版本（演示）" });
      });
      void loadVersion();
    }, 3500);
    return;
  }

  const nodeIds = keys.filter((k) => k !== MASTER_KEY);
  try {
    const res = await UpgradeTargets({ master: keys.includes(MASTER_KEY), nodeIds, force: req.force });
    nodeIds.forEach((id) => applyResult(id, res.agents.find((a) => a.nodeId === id)));
    if (keys.includes(MASTER_KEY)) applyResult(MASTER_KEY, res.master);
  } catch (e) {
    keys.forEach((k) => setStatus(k, { phase: "failed", message: (e as Error).message }));
  }
  ensureWatch();
}

function ensureWatch() {
  if (watchTimer != null) return;
  watchTimer = window.setInterval(() => void watchTick(), WATCH_MS);
}

async function watchTick() {
  const running = Object.entries(state.upgrades).filter(([, s]) => s.phase === "running");
  if (!running.length) {
    if (watchTimer != null) clearInterval(watchTimer);
    watchTimer = null;
    return;
  }
  let agentDone = false;
  await Promise.all(
    running.map(async ([key, s]) => {
      if (Date.now() - s.at > GIVE_UP_MS) {
        setStatus(key, { phase: "failed", message: `等待超时：请到该机器查看升级日志（${s.message}）` });
        return;
      }
      if (key === MASTER_KEY) return watchMaster(s);
      agentDone = (await watchAgent(key, s)) || agentDone;
    }),
  );
  if (agentDone) void loadVersion().catch(() => {});
}

async function watchMaster(s: UpgradeStatus) {
  try {
    const v = await GetVersion();
    if (v.current && v.current !== s.from) {
      setStatus(MASTER_KEY, { phase: "done", message: `已升级到 v${v.current}，即将刷新页面` });
      window.setTimeout(() => location.reload(), RELOAD_DELAY_MS);
    }
  } catch {
    setStatus(MASTER_KEY, { message: "Master 重启中…" });
  }
}

async function watchAgent(id: string, s: UpgradeStatus): Promise<boolean> {
  try {
    const st = await GetNodeStats(id);
    if (st.link !== "ws") {
      setStatus(id, { message: "Agent 重启中…" });
      return false;
    }
    if (st.agentVersion && st.agentVersion !== s.from) {
      setStatus(id, { phase: "done", message: `已升级到 v${st.agentVersion}` });
      return true;
    }
  } catch {
    /* transient; retry next tick */
  }
  return false;
}
