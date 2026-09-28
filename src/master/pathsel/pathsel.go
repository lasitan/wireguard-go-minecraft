// Package pathsel picks, for every node and every VPN subnet it routes to, the
// lowest-latency mother card as its gateway, using tunnel handshake RTTs the
// agents report.
package pathsel

import (
	"fmt"
	"math"
	"os"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/master/stats"
	"golang.zx2c4.com/wireguard/src/master/store"
)

const (
	tickEvery = 15 * time.Second
	// A sample older than this no longer says anything about the path.
	rttFreshness = 10 * time.Minute
	// Hysteresis: only switch when the new gateway is clearly faster.
	switchRatio  = 0.8
	switchMinGap = 3.0 // ms

	scoreUnknown = 1e9
	// Master's relay beats only dead mothers.
	scoreRelay = 1e12
)

// RTTFunc returns the smoothed RTT (ms) node a measured toward node b.
type RTTFunc func(a, b string) (float64, bool)

// Choose keeps the current gateway unless another candidate is clearly faster
// or the current one is gone.
func Choose(cur core.PathChoices, cands map[string]map[string][]string, rtt RTTFunc, alive func(string) bool) core.PathChoices {
	next := core.PathChoices{}
	for node, bySub := range cands {
		for sub, list := range bySub {
			if len(list) == 0 {
				continue
			}
			score := func(m string) float64 {
				if m == core.RelayNodeID {
					return scoreRelay
				}
				if !alive(m) {
					return math.Inf(1)
				}
				if r, ok := rtt(node, m); ok {
					return r
				}
				if r, ok := rtt(m, node); ok {
					return r
				}
				return scoreUnknown
			}
			best, bestScore := list[0], score(list[0])
			for _, m := range list[1:] {
				if s := score(m); s < bestScore {
					best, bestScore = m, s
				}
			}
			choice := best
			if prev := cur[node][sub]; prev != "" && prev != best && contains(list, prev) {
				ps := score(prev)
				if !(bestScore < ps*switchRatio && ps-bestScore > switchMinGap) {
					choice = prev
				}
			}
			if next[node] == nil {
				next[node] = map[string]string{}
			}
			next[node][sub] = choice
		}
	}
	return next
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Selector re-evaluates gateway choices periodically and pushes changes.
type Selector struct {
	Store *store.Store
	Stats *stats.StatsService
	Alive func(nodeID string) bool
	Push  func()
}

func (s *Selector) Run(stop <-chan struct{}) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Tick(time.Now())
		}
	}
}

func (s *Selector) Tick(now time.Time) {
	m := s.Store.Snapshot()
	cands := core.PlanMesh(&m).Candidates()
	pubOf := make(map[string]string, len(m.Nodes))
	for _, n := range m.Nodes {
		pubOf[n.ID] = n.PublicKey
	}
	rtts := s.Stats.PeerRTTs()
	rtt := func(a, b string) (float64, bool) {
		r, ok := rtts[a][pubOf[b]]
		if !ok || r.Millis <= 0 || now.Sub(r.At) > rttFreshness {
			return 0, false
		}
		return r.Millis, true
	}
	alive := s.Alive
	if alive == nil {
		alive = func(string) bool { return true }
	}
	next := Choose(m.Paths, cands, rtt, alive)
	changed, err := s.Store.SetPaths(next)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: path selection: %v\n", err)
		return
	}
	sbChanged, err := s.Store.SetStandby(core.ClusterStandby(&m, alive))
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: cluster standby: %v\n", err)
	}
	if (changed || sbChanged) && s.Push != nil {
		s.Push()
	}
}
