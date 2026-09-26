import type { Meta } from "../core/models";
import { requestApi } from "./requestApi";

export function GetMeta(): Promise<Meta> {
  return requestApi<Meta>("/api/meta");
}
