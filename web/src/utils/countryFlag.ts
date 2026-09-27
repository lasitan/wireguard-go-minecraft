/** Windows ships no flag glyphs; regional indicators render as bare letters. */
const flagsSupported = typeof navigator === "undefined" || !/Windows/i.test(navigator.userAgent);

/** ISO 3166 alpha-2 → flag emoji ("JP" → 🇯🇵), or "" where flags don't render. */
export function countryFlag(code: string | undefined): string {
  if (!flagsSupported || !code || !/^[A-Za-z]{2}$/.test(code)) return "";
  const base = 0x1f1e6 - 65;
  const up = code.toUpperCase();
  return String.fromCodePoint(base + up.charCodeAt(0), base + up.charCodeAt(1));
}
