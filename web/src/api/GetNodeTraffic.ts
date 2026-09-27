import type { TrafficRange, TrafficSeries } from "../core/models";
import { requestApi } from "./requestApi";

export function GetNodeTraffic(id: string, range: TrafficRange): Promise<TrafficSeries> {
  return requestApi<TrafficSeries>(
    "/api/nodes/traffic?id=" + encodeURIComponent(id) + "&range=" + encodeURIComponent(range),
  );
}
