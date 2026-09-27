const V4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

export function isIPv4(s: string): boolean {
  return V4.test(s);
}

function isIPv6(s: string): boolean {
  if (!/^[0-9a-fA-F:.]+$/.test(s) || !s.includes(":")) return false;
  try {
    new URL(`http://[${s}]/`);
    return true;
  } catch {
    return false;
  }
}

/** Returns an error message, or "" when `s` is a valid IPv4/IPv6 CIDR. */
export function validateCidr(s: string, opts: { v4Only?: boolean; allowDefault?: boolean } = {}): string {
  const m = /^([^/]+)\/(\d{1,3})$/.exec(s.trim());
  if (!m) return "格式应为 地址/前缀，如 192.168.1.0/24";
  const [, addr, bitsStr] = m;
  const bits = Number(bitsStr);
  if (isIPv4(addr)) {
    if (bits > 32) return "IPv4 前缀不能大于 32";
  } else if (!opts.v4Only && isIPv6(addr)) {
    if (bits > 128) return "IPv6 前缀不能大于 128";
  } else {
    return opts.v4Only ? "需要 IPv4 地址" : "地址无效";
  }
  if (bits === 0 && !opts.allowDefault) return "不允许默认路由 /0";
  return "";
}

/** Validates a node VPN address like 10.10.0.5/24. */
export function validateNodeAddress(s: string): string {
  const err = validateCidr(s, { v4Only: true });
  if (err) return err;
  const bits = Number(s.split("/")[1]);
  if (bits >= 31) return "前缀过窄（需小于 /31）";
  return "";
}
