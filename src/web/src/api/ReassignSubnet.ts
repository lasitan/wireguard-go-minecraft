import type { Mesh } from "../core/models";
import { requestApi } from "./requestApi";

export function ReassignSubnet(id: string, prefix: string): Promise<Mesh> {
  return requestApi<Mesh>("/api/nodes/reassign-subnet", {
    method: "POST",
    body: JSON.stringify({ id, prefix }),
  });
}
