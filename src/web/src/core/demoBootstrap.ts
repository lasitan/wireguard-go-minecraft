import type { AgentInstallCommands } from "./models";
import { state } from "./state";

const RAW = "https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/deploy/scripts";

/** Demo one-liners (same shape as GET /api/install/agent). */
export function demoAgentInstallCommands(role: "client" | "server", endpoint: string): AgentInstallCommands {
  const masterUrl = typeof window !== "undefined" ? window.location.origin : "https://master.example.com";
  const key = state.meta?.enrollToken || "dev-preview-enroll-token";
  const env = `LASITAN_BOOTSTRAP=agent LASITAN_MASTER_URL='${masterUrl}' LASITAN_ENROLL_KEY='${key}' LASITAN_ROLE='${role}'${
    role === "server"
      ? (endpoint.trim() ? ` LASITAN_ENDPOINT='${endpoint.trim()}'` : "") + " LASITAN_LISTEN_PORT=25590"
      : ""
  }`;
  return {
    masterUrl,
    linux: `curl -fsSL ${RAW}/install.sh | sudo env ${env} bash`,
    linuxCn: `curl -fsSL https://ghfast.top/${RAW}/install.sh | sudo env ${env} LASITAN_GH_PROXY=https://ghfast.top/ bash`,
    windows: `powershell -ExecutionPolicy Bypass -c "$env:LASITAN_BOOTSTRAP='agent'; $env:LASITAN_MASTER_URL='${masterUrl}'; $env:LASITAN_ENROLL_KEY='${key}'; $env:LASITAN_ROLE='${role}'; irm ${RAW}/install.ps1 | iex"`,
  };
}
