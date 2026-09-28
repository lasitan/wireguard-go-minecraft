package stats

import (
	"encoding/base64"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/master/store"

	"golang.zx2c4.com/wireguard/src/core/wire"
)

const (
	statsFlushInterval  = 10 * time.Second
	statsRollupInterval = time.Hour
)

// LiveStats is the in-memory, not-persisted view of a node's current state.
type LiveStats struct {
	RxRate      float64         `json:"rxRate"` // bytes/sec
	TxRate      float64         `json:"txRate"`
	SampleAt    time.Time       `json:"sampleAt"`
	Peers       []LivePeer      `json:"peers"`
	Forwards    []LiveForward   `json:"forwards"`
	ConnectedAt time.Time       `json:"connectedAt"`
	RTTMillis   float64         `json:"rttMs,omitempty"`
	IPRates     map[string]Rate `json:"-"`
}

type Rate struct{ Rx, Tx float64 }

type LivePeer struct {
	PublicKey     string    `json:"publicKey"`
	RxBytes       uint64    `json:"rx"`
	TxBytes       uint64    `json:"tx"`
	LastHandshake time.Time `json:"lastHandshake,omitempty"`
	RTTMillis     float64   `json:"rttMs,omitempty"` // smoothed handshake RTT
}

// PeerRTT is the smoothed tunnel RTT a node measured toward one peer.
type PeerRTT struct {
	Millis float64
	At     time.Time // last sample
}

type peerRTTState struct {
	PeerRTT
	hs int64 // handshake timestamp of the last folded sample
}

const rttSmoothing = 0.3

type LiveForward struct {
	Protocol string `json:"protocol"`
	Listen   string `json:"listen"`
	RxBytes  uint64 `json:"rx"`
	TxBytes  uint64 `json:"tx"`
}

type nodeCounters struct {
	at     time.Time
	rx, tx uint64
	ips    map[string][2]uint64
	live   LiveStats
}

// StatsService turns cumulative agent counters into deltas, rates, and batched writes.
type StatsService struct {
	store *store.Store

	mu        sync.Mutex
	last      map[string]*nodeCounters
	pending   map[string]*store.TrafficDelta
	connected map[string]time.Time
	rtt       map[string]time.Duration
	peerRTT   map[string]map[string]*peerRTTState // nodeID -> peer pubkey
}

func NewStatsService(st *store.Store) *StatsService {
	return &StatsService{
		store:     st,
		last:      make(map[string]*nodeCounters),
		pending:   make(map[string]*store.TrafficDelta),
		connected: make(map[string]time.Time),
		rtt:       make(map[string]time.Duration),
		peerRTT:   make(map[string]map[string]*peerRTTState),
	}
}

// foldPeerRTT smooths one handshake RTT sample; a sample is new only when the
// handshake timestamp moved.
func (s *StatsService) foldPeerRTT(nodeID, pub string, p wire.PeerStat, now time.Time) float64 {
	byPeer := s.peerRTT[nodeID]
	if byPeer == nil {
		byPeer = make(map[string]*peerRTTState)
		s.peerRTT[nodeID] = byPeer
	}
	cur := byPeer[pub]
	if p.HandshakeRTTMicros == 0 {
		if cur == nil {
			return 0
		}
		return cur.Millis
	}
	sample := float64(p.HandshakeRTTMicros) / 1000
	switch {
	case cur == nil:
		byPeer[pub] = &peerRTTState{PeerRTT: PeerRTT{Millis: sample, At: now}, hs: p.LastHandshakeNano}
		return sample
	case cur.hs != p.LastHandshakeNano:
		cur.Millis += rttSmoothing * (sample - cur.Millis)
		cur.At = now
		cur.hs = p.LastHandshakeNano
	}
	return cur.Millis
}

// PeerRTTs snapshots every node's smoothed RTT per peer public key.
func (s *StatsService) PeerRTTs() map[string]map[string]PeerRTT {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]map[string]PeerRTT, len(s.peerRTT))
	for id, byPeer := range s.peerRTT {
		m := make(map[string]PeerRTT, len(byPeer))
		for pub, st := range byPeer {
			m[pub] = st.PeerRTT
		}
		out[id] = m
	}
	return out
}

// counterDelta handles agent restarts: a counter going backwards means the
// process restarted, so the new value itself is the delta since restart.
func counterDelta(prev, cur uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

func (s *StatsService) Ingest(nodeID string, st *wire.Stats, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.last[nodeID]
	cur := &nodeCounters{at: now, rx: st.RxBytes, tx: st.TxBytes, ips: make(map[string][2]uint64, len(st.IPs))}
	for _, ip := range st.IPs {
		if ip.IP.IsValid() {
			cur.ips[ip.IP.Unmap().String()] = [2]uint64{ip.RxBytes, ip.TxBytes}
		}
	}
	cur.live = LiveStats{SampleAt: now, IPRates: map[string]Rate{}}
	for _, p := range st.Peers {
		lp := LivePeer{
			PublicKey: base64.StdEncoding.EncodeToString(p.PublicKey[:]),
			RxBytes:   p.RxBytes,
			TxBytes:   p.TxBytes,
		}
		if p.LastHandshakeNano > 0 {
			lp.LastHandshake = time.Unix(0, p.LastHandshakeNano).UTC()
		}
		lp.RTTMillis = s.foldPeerRTT(nodeID, lp.PublicKey, p, now)
		cur.live.Peers = append(cur.live.Peers, lp)
	}
	for _, f := range st.Forwards {
		cur.live.Forwards = append(cur.live.Forwards, LiveForward{Protocol: f.Protocol, Listen: f.Listen, RxBytes: f.RxBytes, TxBytes: f.TxBytes})
	}

	if prev == nil {
		// First sample after (re)connect: this is the baseline, no delta/rate yet.
		s.last[nodeID] = cur
		return
	}

	dt := now.Sub(prev.at).Seconds()
	drx := counterDelta(prev.rx, cur.rx)
	dtx := counterDelta(prev.tx, cur.tx)
	if dt > 0 {
		cur.live.RxRate = float64(drx) / dt
		cur.live.TxRate = float64(dtx) / dt
	}

	d := s.pending[nodeID]
	if d == nil {
		d = &store.TrafficDelta{Minutes: map[int64][2]uint64{}, IPs: map[string][2]uint64{}}
		s.pending[nodeID] = d
	}
	d.Rx += drx
	d.Tx += dtx
	minute := now.Truncate(time.Minute).Unix()
	mv := d.Minutes[minute]
	d.Minutes[minute] = [2]uint64{mv[0] + drx, mv[1] + dtx}
	for ip, v := range cur.ips {
		pv := prev.ips[ip]
		irx, itx := counterDelta(pv[0], v[0]), counterDelta(pv[1], v[1])
		if irx == 0 && itx == 0 {
			continue
		}
		acc := d.IPs[ip]
		d.IPs[ip] = [2]uint64{acc[0] + irx, acc[1] + itx}
		if dt > 0 {
			cur.live.IPRates[ip] = Rate{Rx: float64(irx) / dt, Tx: float64(itx) / dt}
		}
	}
	s.last[nodeID] = cur
}

// MarkConnected drops the counter baseline so the first sample after a
// reconnect (agent counters may have reset) is not treated as a delta.
func (s *StatsService) MarkConnected(nodeID string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.last, nodeID)
	s.connected[nodeID] = now
}

func (s *StatsService) MarkDisconnected(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connected, nodeID)
	delete(s.rtt, nodeID)
	if c := s.last[nodeID]; c != nil {
		c.live.RxRate, c.live.TxRate = 0, 0
		c.live.IPRates = map[string]Rate{}
	}
}

func (s *StatsService) SetRTT(nodeID string, rtt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rtt[nodeID] = rtt
}

// Live returns a copy of the current live stats (zero value if none).
func (s *StatsService) Live(nodeID string) (LiveStats, map[string]Rate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out LiveStats
	var ir map[string]Rate
	if c := s.last[nodeID]; c != nil {
		out = c.live
		ir = make(map[string]Rate, len(c.live.IPRates))
		for k, v := range c.live.IPRates {
			ir[k] = v
		}
		// Stale samples (agent went quiet) should not keep showing a rate.
		if time.Since(out.SampleAt) > 3*statsFlushInterval {
			out.RxRate, out.TxRate = 0, 0
			ir = nil
		}
	}
	out.IPRates = nil
	out.ConnectedAt = s.connected[nodeID]
	if r, ok := s.rtt[nodeID]; ok {
		out.RTTMillis = float64(r.Microseconds()) / 1000
	}
	return out, ir
}

func (s *StatsService) IsConnected(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.connected[nodeID]
	return ok
}

func (s *StatsService) Forget(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.last, nodeID)
	delete(s.pending, nodeID)
	delete(s.connected, nodeID)
	delete(s.rtt, nodeID)
	delete(s.peerRTT, nodeID)
}

func (s *StatsService) takePending() map[string]*store.TrafficDelta {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	p := s.pending
	s.pending = make(map[string]*store.TrafficDelta)
	return p
}

// Run flushes batched deltas and performs periodic rollup until stop closes.
func (s *StatsService) Run(stop <-chan struct{}) {
	flush := time.NewTicker(statsFlushInterval)
	rollup := time.NewTicker(statsRollupInterval)
	defer flush.Stop()
	defer rollup.Stop()
	if err := s.store.RollupAndPrune(time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: traffic rollup: %v\n", err)
	}
	for {
		select {
		case <-stop:
			s.FlushNow()
			return
		case <-flush.C:
			s.FlushNow()
		case <-rollup.C:
			if err := s.store.RollupAndPrune(time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "lasitan-cluster master: traffic rollup: %v\n", err)
			}
		}
	}
}

func (s *StatsService) FlushNow() {
	p := s.takePending()
	if p == nil {
		return
	}
	if err := s.store.FlushTraffic(p, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: traffic flush: %v\n", err)
		// Put deltas back so they are retried on the next tick.
		s.mu.Lock()
		for id, d := range p {
			cur := s.pending[id]
			if cur == nil {
				s.pending[id] = d
				continue
			}
			cur.Rx += d.Rx
			cur.Tx += d.Tx
			for k, v := range d.Minutes {
				c := cur.Minutes[k]
				cur.Minutes[k] = [2]uint64{c[0] + v[0], c[1] + v[1]}
			}
			for k, v := range d.IPs {
				c := cur.IPs[k]
				cur.IPs[k] = [2]uint64{c[0] + v[0], c[1] + v[1]}
			}
		}
		s.mu.Unlock()
	}
}
