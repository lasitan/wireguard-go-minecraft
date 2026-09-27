import type { Meta, MetaPatch } from "../core/models";
import { requestApi } from "./requestApi";

export function PatchMeta(patch: MetaPatch): Promise<Meta> {
  return requestApi<Meta>("/api/meta", {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}
