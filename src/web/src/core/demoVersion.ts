import type { Mesh, VersionInfo } from "./models";
import { demoLink } from "./demoStats";
import { isNewerVersion } from "../utils/compareVersion";

const REPO = "https://github.com/lasitan/wireguard-go-minecraft";
const RAW = "https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/scripts";
const DEMO_CURRENT = "2.1.0";
const DEMO_LATEST = "2.1.1";

/** MacBook Pro lags one release behind so the "outdated" UI has something to show. */
const DEMO_AGENT_VERSIONS: Record<string, string> = {
  "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003": DEMO_CURRENT,
};

export function demoAgentVersion(id: string): string {
  return DEMO_AGENT_VERSIONS[id] ?? DEMO_LATEST;
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
    hasUpdate: true,
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
      linuxCn: `curl -fsSL https://ghfast.top/${RAW}/install.sh | sudo WG_MC_GH_PROXY=https://ghfast.top/ bash`,
      installed: "sudo wireguard-go update",
      windows: `powershell -ExecutionPolicy Bypass -c "irm ${RAW}/install.ps1 | iex"`,
    },
    outdatedAgents: outdated,
  };
}
