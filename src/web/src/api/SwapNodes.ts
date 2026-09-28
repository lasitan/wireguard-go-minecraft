import type { Mesh } from "../core/models";
import { requestApi } from "./requestApi";

export function SwapNodes(a: string, b: string): Promise<Mesh> {
  return requestApi<Mesh>("/api/nodes/swap", {
    method: "POST",
    body: JSON.stringify({ a, b }),
  });
}
