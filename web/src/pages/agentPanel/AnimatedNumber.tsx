import { useEffect, useRef, useState } from "react";

/** Rolls smoothly from the previous value to `value` (ease-out cubic). */
export function AnimatedNumber({
  value,
  format,
  duration = 650,
}: {
  value: number;
  format: (v: number) => string;
  duration?: number;
}) {
  const [shown, setShown] = useState(value);
  const shownRef = useRef(value);

  useEffect(() => {
    const from = shownRef.current;
    if (from === value) return;
    const start = performance.now();
    let raf = 0;
    const tick = (now: number) => {
      const t = Math.min(1, (now - start) / duration);
      const eased = 1 - Math.pow(1 - t, 3);
      const v = from + (value - from) * eased;
      shownRef.current = v;
      setShown(v);
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [value, duration]);

  return <span className="num-roll">{format(shown)}</span>;
}
