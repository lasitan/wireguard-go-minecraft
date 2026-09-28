import type { UpgradeRequest, UpgradeResponse } from "../core/models";
import { requestApi } from "./requestApi";

/** Starts self-updaters on the Master and/or agents; each result only means "updater started". */
export function UpgradeTargets(req: UpgradeRequest): Promise<UpgradeResponse> {
  return requestApi<UpgradeResponse>("/api/version/upgrade", {
    method: "POST",
    body: JSON.stringify(req),
  });
}
