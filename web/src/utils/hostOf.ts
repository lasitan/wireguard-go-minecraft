/** Strip CIDR suffix; empty address becomes an em dash for UI. */
export function hostOf(addr: string): string {
  if (!addr) return "—";
  const i = addr.indexOf("/");
  return i >= 0 ? addr.slice(0, i) : addr;
}

/** Host only for comparison (empty → ""). */
export function hostKey(addr: string): string {
  if (!addr) return "";
  const i = addr.indexOf("/");
  return i >= 0 ? addr.slice(0, i) : addr;
}
