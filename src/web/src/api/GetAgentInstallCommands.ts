import type { AgentInstallCommands } from "../core/models";
import { requestApi } from "./requestApi";

export function GetAgentInstallCommands(role: "client" | "server", endpoint: string): Promise<AgentInstallCommands> {
  const q = new URLSearchParams({ role });
  const ep = endpoint.trim();
  if (ep) q.set("endpoint", ep);
  return requestApi<AgentInstallCommands>("/api/install/agent?" + q.toString());
}
