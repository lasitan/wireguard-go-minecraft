import type { VersionInfo } from "../core/models";
import { requestApi } from "./requestApi";

/** Asks the Master to query GitHub now; the fresh result shows up on a later GET. */
export function RecheckVersion(): Promise<VersionInfo> {
  return requestApi<VersionInfo>("/api/version", { method: "POST" });
}
