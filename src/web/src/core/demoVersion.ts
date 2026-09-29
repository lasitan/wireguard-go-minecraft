import type { Mesh, VersionInfo } from "./models";
import { demoLink } from "./demoStats";
import { isNewerVersion } from "../utils/compareVersion";

const REPO = "https://github.com/lasitan/wireguard-go-minecraft";
const RAW = "https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/scripts";
const DEMO_LATEST = "2.1.1";
let DEMO_CURRENT = "2.1.0";

/** MacBook Pro lags one release behind so the "outdated" UI has something to show. */
const DEMO_AGENT_VERSIONS: Record<string, string> = {
  "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003": DEMO_CURRENT,
};

export function demoAgentVersion(id: string): string {
  return DEMO_AGENT_VERSIONS[id] ?? DEMO_LATEST;
}

/** Demo stand-in for a finished web-triggered upgrade ("master" or a node id). */
export function demoUpgrade(key: string) {
  if (key === "master") DEMO_CURRENT = DEMO_LATEST;
  else DEMO_AGENT_VERSIONS[key] = DEMO_LATEST;
}

export function demoVersion(mesh: Mesh | null): VersionInfo {
  const outdated = (mesh?.nodes || []).flatMap((n) => {
    const link = demoLink(n);
    if (link === "offline") return [];
    const version = link === "ws" ? demoAgentVersion(n.id) : "";
    if (version && !isNewerVersion(DEMO_LATEST, version)) return [];
    return [{ nodeId: n.id, name: n.name || n.id, version }];
  });
  return {
    current: DEMO_CURRENT,
    latest: DEMO_LATEST,
    hasUpdate: isNewerVersion(DEMO_LATEST, DEMO_CURRENT),
    release: {
      tag: "v" + DEMO_LATEST,
      version: DEMO_LATEST,
      url: `${REPO}/releases/tag/v${DEMO_LATEST}`,
      notes: "",
      publishedAt: new Date(Date.now() - 3 * 3600_000).toISOString(),
    },
    checkedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
    releasesUrl: REPO + "/releases",
    commands: {
      linux: `curl -fsSL ${RAW}/install.sh | sudo bash`,
      linuxCn: `curl -fsSL https://ghfast.top/${RAW}/install.sh | sudo LASITAN_GH_PROXY=https://ghfast.top/ bash`,
      installed: "sudo lasitan-cluster update",
      windows: `powershell -ExecutionPolicy Bypass -c "irm ${RAW}/install.ps1 | iex"`,
    },
    outdatedAgents: outdated,
  };
}
