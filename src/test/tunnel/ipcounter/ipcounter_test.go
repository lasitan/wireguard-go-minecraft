package ipcounter_test

import (
	. "golang.zx2c4.com/wireguard/src/tunnel/ipcounter"
	"net/netip"
	"testing"
)

func ipv4Packet(src, dst string, size int) []byte {
	p := make([]byte, size)
	p[0] = 0x45
	s, d := netip.MustParseAddr(src).As4(), netip.MustParseAddr(dst).As4()
	copy(p[12:16], s[:])
	copy(p[16:20], d[:])
	return p
}

func TestIPCounter(t *testing.T) {
	c := NewIPCounter()
	c.CountOutbound(ipv4Packet("10.10.0.1", "10.10.0.2", 100))
	c.CountOutbound(ipv4Packet("10.10.0.1", "10.10.0.3", 60))
	c.CountInbound(ipv4Packet("10.10.0.2", "10.10.0.1", 40))
	c.CountInbound([]byte{0x45}) // truncated: totals only

	rx, tx, ips := c.Snapshot()
	if rx != 41 || tx != 160 {
		t.Fatalf("totals rx=%d tx=%d", rx, tx)
	}
	got := map[string][2]uint64{}
	for _, s := range ips {
		got[s.IP.String()] = [2]uint64{s.Rx, s.Tx}
	}
	if got["10.10.0.2"] != [2]uint64{40, 100} || got["10.10.0.3"] != [2]uint64{0, 60} {
		t.Fatalf("per-ip: %v", got)
	}
}
