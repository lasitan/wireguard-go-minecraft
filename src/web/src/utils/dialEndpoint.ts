import type { Node } from "../core/models";

function hasPort(ep: string): boolean {
  return /^\[.*\]:\d+$/.test(ep) || /^[^:]+:\d+$/.test(ep);
}

/** Address children dial; mirrors Master's Node.DialEndpoint. "" = unknown. */
export function dialEndpoint(node: Node): string {
  const port = node.listenPort ?? 0;
  const ep = (node.endpoint || "").trim();
  if (ep) {
    if (hasPort(ep) || !port) return ep;
    const host = ep.replace(/^\[|\]$/g, "");
    return host.includes(":") ? `[${host}]:${port}` : `${host}:${port}`;
  }
  if (!port || !node.publicV4) return "";
  return `${node.publicV4}:${port}`;
}
