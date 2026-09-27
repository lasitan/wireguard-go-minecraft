import type { Forward } from "../core/models";
import { requestApi } from "./requestApi";

export function PutNodeForwards(id: string, forwards: Forward[]): Promise<Forward[]> {
  return requestApi<Forward[]>("/api/nodes/forwards?id=" + encodeURIComponent(id), {
    method: "PUT",
    body: JSON.stringify(forwards),
  });
}
