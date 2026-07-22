//go:build !windows

package main

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestIsNestedUpstreamDialIPv4(t *testing.T) {
	up := netip.MustParseAddr("203.0.113.10")
	upPort := uint16(25565)

	// IPv4 TCP to upstream:port
	pkt := make([]byte, 40)
	pkt[0] = 0x45 // v4, IHL=5
	pkt[9] = 6    // TCP
	copy(pkt[16:20], up.AsSlice())
	binary.BigEndian.PutUint16(pkt[22:24], upPort) // dest port at ihl+2
	if !isNestedUpstreamDial(pkt, up, upPort) {
		t.Fatal("expected drop for TCP nested dial")
	}

	// wrong port
	binary.BigEndian.PutUint16(pkt[22:24], 80)
	if isNestedUpstreamDial(pkt, up, upPort) {
		t.Fatal("should not match wrong port")
	}

	// UDP nested
	binary.BigEndian.PutUint16(pkt[22:24], upPort)
	pkt[9] = 17
	if !isNestedUpstreamDial(pkt, up, upPort) {
		t.Fatal("expected drop for UDP nested dial")
	}

	// different dest IP
	pkt[19] = 11
	if isNestedUpstreamDial(pkt, up, upPort) {
		t.Fatal("should not match other dest")
	}
}
