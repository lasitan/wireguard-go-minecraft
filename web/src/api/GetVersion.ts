import type { VersionInfo } from "../core/models";
import { requestApi } from "./requestApi";

export function GetVersion(): Promise<VersionInfo> {
  return requestApi<VersionInfo>("/api/version");
}
