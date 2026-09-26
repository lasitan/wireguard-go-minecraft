import type { Mesh } from "../core/models";
import { requestApi } from "./requestApi";

export function GetMesh(): Promise<Mesh> {
  return requestApi<Mesh>("/api/mesh");
}
