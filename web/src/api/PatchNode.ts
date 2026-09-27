import type { Mesh, NodePatch } from "../core/models";
import { requestApi } from "./requestApi";

export function PatchNode(id: string, patch: NodePatch): Promise<Mesh> {
  return requestApi<Mesh>("/api/nodes?id=" + encodeURIComponent(id), {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}
