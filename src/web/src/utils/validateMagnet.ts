/** "" = not a mother card. */
export function validateListenPort(raw: string): string {
  const s = raw.trim();
  if (!s) return "";
  if (!/^\d+$/.test(s)) return "端口必须是数字";
  const n = Number(s);
  return n >= 1 && n <= 65535 ? "" : "端口范围 1–65535";
}

/** "" = auto (public IPv4 + listen port). Accepts host, host:port, [v6]:port. */
export function validateEndpoint(raw: string): string {
  const s = raw.trim();
  if (!s) return "";
  if (/[\s/]/.test(s)) return "格式为 host 或 host:端口";
  const m = /^\[([^\]]+)\]:(\d+)$/.exec(s) || /^([^:]+):(\d+)$/.exec(s);
  if (m) {
    const port = Number(m[2]);
    return port >= 1 && port <= 65535 ? "" : "端口范围 1–65535";
  }
  if (s.includes(":") && !/^[0-9a-fA-F:]+$/.test(s)) return "格式为 host 或 host:端口";
  return "";
}
