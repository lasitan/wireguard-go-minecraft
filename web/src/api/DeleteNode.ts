import type { Mesh } from "../core/models";
import { requestApi } from "./requestApi";

export function DeleteNode(id: string): Promise<Mesh> {
  return requestApi<Mesh>("/api/nodes?id=" + encodeURIComponent(id), { method: "DELETE" });
}
