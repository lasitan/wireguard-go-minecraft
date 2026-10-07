package hub

import (
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/master/stats"
)

func (h *Hub) Stats() *stats.StatsService {
	return h.stats
}

// QueuedRevisionAfter queues the given revisions on a fresh connection, in
// order, and returns the revision left pending for the writer.
func QueuedRevisionAfter(revs ...int) int {
	c := &agentConn{cfgSig: make(chan struct{}, 1)}
	for _, r := range revs {
		c.queueConfig(&core.DesiredConfig{Revision: r})
	}
	return c.cfg.Load().Revision
}
