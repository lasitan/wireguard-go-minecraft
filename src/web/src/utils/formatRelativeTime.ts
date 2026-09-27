/** "刚刚" / "3 分钟前" / "2 天前"; empty or zero time → "从未". */
export function formatRelativeTime(iso: string | undefined, now = Date.now()): string {
  const t = parseTime(iso);
  if (t == null) return "从未";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 10) return "刚刚";
  if (s < 60) return `${s} 秒前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} 小时前`;
  return `${Math.floor(h / 24)} 天前`;
}

/** Absolute local time "2026-09-27 14:03:11". */
export function formatDateTime(iso: string | undefined): string {
  const t = parseTime(iso);
  if (t == null) return "—";
  const d = new Date(t);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function parseTime(iso: string | undefined): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  // Go zero time marshals as 0001-01-01.
  if (!Number.isFinite(t) || t <= 0) return null;
  return t;
}
