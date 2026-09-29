import { routePcbBundle, type Rect, type RouteRequest } from "./PcbRouter";

export type RouteJob = {
  key: string;
  requests: RouteRequest[];
  obstacles: [string, Rect][];
  master: Rect;
};

export type RouteResult = { key: string; paths: [string, string][] };

self.onmessage = (ev: MessageEvent<RouteJob>) => {
  const job = ev.data;
  const paths = routePcbBundle(job.requests, new Map(job.obstacles), job.master);
  (self as unknown as Worker).postMessage({ key: job.key, paths: [...paths] } satisfies RouteResult);
};
