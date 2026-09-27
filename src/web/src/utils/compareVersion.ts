type Parsed = { nums: [number, number, number]; pre: string };

function parse(v: string | undefined): Parsed | null {
  if (!v) return null;
  let s = v.trim().replace(/^v/, "");
  let pre = "";
  const cut = s.search(/[-+]/);
  if (cut >= 0) {
    pre = s.slice(cut + 1);
    s = s.slice(0, cut);
  }
  const parts = s.split(".");
  if (parts.length === 0 || parts.length > 3 || parts.some((p) => !/^\d+$/.test(p))) return null;
  const nums: [number, number, number] = [0, 0, 0];
  parts.forEach((p, i) => (nums[i] = Number(p)));
  return { nums, pre };
}

/** Mirrors Go update.Newer: unparseable versions never count as newer. */
export function isNewerVersion(candidate: string | undefined, base: string | undefined): boolean {
  const c = parse(candidate);
  const b = parse(base);
  if (!c || !b) return false;
  for (let i = 0; i < 3; i++) {
    if (c.nums[i] !== b.nums[i]) return c.nums[i] > b.nums[i];
  }
  return c.pre === "" && b.pre !== "";
}
