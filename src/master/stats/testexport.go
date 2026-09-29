package stats

import "golang.zx2c4.com/wireguard/src/master/store"

func (s *StatsService) TakePending() map[string]*store.TrafficDelta {
	return s.takePending()
}
