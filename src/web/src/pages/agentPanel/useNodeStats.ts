import { useEffect, useState } from "react";
import { isLive, watchNodeStats } from "../../app/LiveSocket";
import { fetchNodeStats } from "../../app/NodeActions";
import type { NodeStats } from "../../core/models";

export const STATS_POLL_MS = 2000;

/**
 * Live stats while the panel for `id` is open: pushed by the Master over the
 * UI socket, or polled every 2s when the socket is down (and in demo mode).
 */
export function useNodeStats(id: string): { stats: NodeStats | null; error: string } {
  const [stats, setStats] = useState<NodeStats | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    setStats(null);
    setError("");
    const unwatch = watchNodeStats(id, (s) => {
      if (!alive) return;
      setStats(s);
      setError("");
    });
    const load = () => {
      if (isLive()) return;
      fetchNodeStats(id)
        .then((s) => {
          if (!alive) return;
          setStats(s);
          setError("");
        })
        .catch((e: Error) => alive && setError(e.message));
    };
    load();
    const timer = window.setInterval(load, STATS_POLL_MS);
    return () => {
      alive = false;
      unwatch();
      window.clearInterval(timer);
    };
  }, [id]);

  return { stats, error };
}
