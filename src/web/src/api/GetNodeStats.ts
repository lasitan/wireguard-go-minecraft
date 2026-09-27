import type { NodeStats } from "../core/models";
import { requestApi } from "./requestApi";

export function GetNodeStats(id: string): Promise<NodeStats> {
  return requestApi<NodeStats>("/api/nodes/stats?id=" + encodeURIComponent(id));
}
