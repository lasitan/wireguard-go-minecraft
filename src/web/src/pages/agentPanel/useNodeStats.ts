import { useEffect, useState } from "react";
import { fetchNodeStats } from "../../app/NodeActions";
import type { NodeStats } from "../../core/models";

export const STATS_POLL_MS = 2000;

/** Polls live stats every 2s while the panel for `id` is open. */
export function useNodeStats(id: string): { stats: NodeStats | null; error: string } {
  const [stats, setStats] = useState<NodeStats | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    setStats(null);
    setError("");
    const load = () => {
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
      window.clearInterval(timer);
    };
  }, [id]);

  return { stats, error };
}
