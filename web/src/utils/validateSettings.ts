import { validateCidr } from "./validateCidr";

/** Mirrors Master's normalizeEnrollToken. */
export function validateEnrollToken(s: string): string {
  const v = s.trim();
  if (v.length < 8) return "至少 8 个字符";
  if (v.length > 128) return "最多 128 个字符";
  if (/[\s"'\\]/.test(v)) return "不能包含空格、引号或反斜杠";
  return "";
}

/** Mirrors Master's normalizePool: IPv4, /8../30. */
export function validatePool(s: string): string {
  const err = validateCidr(s, { v4Only: true });
  if (err) return err;
  const bits = Number(s.trim().split("/")[1]);
  if (bits < 8 || bits > 30) return "前缀需在 /8 到 /30 之间";
  return "";
}

/** Masks host bits, e.g. 10.20.3.4/16 → 10.20.0.0/16. */
export function maskPool(s: string): string {
  const [addr, bitsStr] = s.trim().split("/");
  const bits = Number(bitsStr);
  const n = addr.split(".").reduce((acc, o) => ((acc << 8) | Number(o)) >>> 0, 0);
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  const m = (n & mask) >>> 0;
  return `${m >>> 24}.${(m >>> 16) & 255}.${(m >>> 8) & 255}.${m & 255}/${bits}`;
}

export function randomEnrollToken(): string {
  const b = new Uint8Array(18);
  crypto.getRandomValues(b);
  return btoa(String.fromCharCode(...b)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
