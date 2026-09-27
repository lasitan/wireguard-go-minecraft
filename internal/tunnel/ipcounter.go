package tunnel

import (
	"net/netip"
	"sync"
	"sync/atomic"
)

// maxTrackedIPs bounds memory if something floods the tunnel with spoofed
// addresses; packets for new IPs beyond the cap are only counted in totals.
const maxTrackedIPs = 8192

type ipCounters struct {
	rx atomic.Uint64
	tx atomic.Uint64
}

// IPCounter implements device.TrafficCounter, keyed by the remote VPN IP:
// destination for outbound packets and source for inbound ones.
type IPCounter struct {
	rx, tx atomic.Uint64
	ips    sync.Map // netip.Addr -> *ipCounters
	n      atomic.Int32
}

func NewIPCounter() *IPCounter { return &IPCounter{} }

func (c *IPCounter) CountOutbound(packet []byte) {
	c.tx.Add(uint64(len(packet)))
	if a, ok := packetAddr(packet, false); ok {
		if e := c.entry(a); e != nil {
			e.tx.Add(uint64(len(packet)))
		}
	}
}

func (c *IPCounter) CountInbound(packet []byte) {
	c.rx.Add(uint64(len(packet)))
	if a, ok := packetAddr(packet, true); ok {
		if e := c.entry(a); e != nil {
			e.rx.Add(uint64(len(packet)))
		}
	}
}

func (c *IPCounter) entry(a netip.Addr) *ipCounters {
	if v, ok := c.ips.Load(a); ok {
		return v.(*ipCounters)
	}
	if c.n.Load() >= maxTrackedIPs {
		return nil
	}
	v, loaded := c.ips.LoadOrStore(a, &ipCounters{})
	if !loaded {
		c.n.Add(1)
	}
	return v.(*ipCounters)
}

// packetAddr extracts the source (src=true) or destination address.
func packetAddr(p []byte, src bool) (netip.Addr, bool) {
	if len(p) < 1 {
		return netip.Addr{}, false
	}
	switch p[0] >> 4 {
	case 4:
		if len(p) < 20 {
			return netip.Addr{}, false
		}
		off := 16
		if src {
			off = 12
		}
		return netip.AddrFrom4([4]byte(p[off : off+4])), true
	case 6:
		if len(p) < 40 {
			return netip.Addr{}, false
		}
		off := 24
		if src {
			off = 8
		}
		return netip.AddrFrom16([16]byte(p[off : off+16])), true
	}
	return netip.Addr{}, false
}

type IPTraffic struct {
	IP     netip.Addr
	Rx, Tx uint64
}

// Snapshot returns cumulative totals and per-IP counters.
func (c *IPCounter) Snapshot() (rx, tx uint64, ips []IPTraffic) {
	c.ips.Range(func(k, v any) bool {
		e := v.(*ipCounters)
		ips = append(ips, IPTraffic{IP: k.(netip.Addr), Rx: e.rx.Load(), Tx: e.tx.Load()})
		return true
	})
	return c.rx.Load(), c.tx.Load(), ips
}
