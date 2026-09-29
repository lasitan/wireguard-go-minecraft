package hub

import "golang.zx2c4.com/wireguard/src/master/stats"

func (h *Hub) Stats() *stats.StatsService {
	return h.stats
}
