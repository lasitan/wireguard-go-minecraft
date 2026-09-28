import type { AgentInstallCommands } from "./models";
import { state } from "./state";

const RAW = "https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/deploy/scripts";

/** Demo one-liners (same shape as GET /api/install/agent). */
export function demoAgentInstallCommands(role: "client" | "server", endpoint: string): AgentInstallCommands {
  const masterUrl = typeof window !== "undefined" ? window.location.origin : "https://master.example.com";
  const key = state.meta?.enrollToken || "dev-preview-enroll-token";
  const env = `WG_MC_BOOTSTRAP=agent WG_MC_MASTER_URL='${masterUrl}' WG_MC_ENROLL_KEY='${key}' WG_MC_ROLE='${role}'${
    role === "server"
      ? (endpoint.trim() ? ` WG_MC_ENDPOINT='${endpoint.trim()}'` : "") + " WG_MC_LISTEN_PORT=25590"
      : ""
  }`;
  return {
    masterUrl,
    linux: `curl -fsSL ${RAW}/install.sh | sudo env ${env} bash`,
    linuxCn: `curl -fsSL https://ghfast.top/${RAW}/install.sh | sudo env ${env} WG_MC_GH_PROXY=https://ghfast.top/ bash`,
    windows: `powershell -ExecutionPolicy Bypass -c "$env:WG_MC_BOOTSTRAP='agent'; $env:WG_MC_MASTER_URL='${masterUrl}'; $env:WG_MC_ENROLL_KEY='${key}'; $env:WG_MC_ROLE='${role}'; irm ${RAW}/install.ps1 | iex"`,
  };
}
