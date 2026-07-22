/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/device"
)

// natRuntime holds dynamically discovered ToNAT clients (shared by filter + MASQUERADE).
type natRuntime struct {
	mu         sync.Mutex
	clientKeys map[device.NoisePublicKey]struct{}
	hosts      map[string]struct{}
}

func newNatRuntime(seedKeys []string) *natRuntime {
	r := &natRuntime{
		clientKeys: make(map[device.NoisePublicKey]struct{}),
		hosts:      make(map[string]struct{}),
	}
	for _, hx := range seedKeys {
		var pk device.NoisePublicKey
		raw, err := hex.DecodeString(hx)
		if err != nil || len(raw) != device.NoisePublicKeySize {
			continue
		}
		copy(pk[:], raw)
		r.clientKeys[pk] = struct{}{}
	}
	return r
}

func (r *natRuntime) hasKey(pk device.NoisePublicKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.clientKeys[pk]
	return ok
}

func (r *natRuntime) add(pk device.NoisePublicKey, hosts []string) (isNew bool, newHosts []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.clientKeys[pk]; !ok {
		r.clientKeys[pk] = struct{}{}
		isNew = true
	}
	for _, h := range hosts {
		if _, ok := r.hosts[h]; ok {
			continue
		}
		r.hosts[h] = struct{}{}
		newHosts = append(newHosts, h)
	}
	return isNew, newHosts
}

func installNatInboundFilter(dev *device.Device, rt *natRuntime, upstream netip.AddrPort, logger *device.Logger) {
	upIP := upstream.Addr()
	upPort := upstream.Port()
	upStr := upstream.String()

	dev.SetInboundPacketFilter(func(peerKey device.NoisePublicKey, packet []byte) bool {
		if !rt.hasKey(peerKey) {
			return true
		}
		if isNestedUpstreamDial(packet, upIP, upPort) {
			logger.Verbosef("NAT gateway: dropped nested dial to upstream %s from NatClient", upStr)
			return false
		}
		return true
	})
}

func (g *natGateway) RegisterClient(peerKey device.NoisePublicKey) {
	if g == nil || !g.cfg.serverMode {
		return
	}
	hx := hex.EncodeToString(peerKey[:])
	hosts := g.cfg.peersByKeyHex[hx]
	if len(hosts) == 0 {
		// Peer not in conf AllowedIPs — still mark for nested-block filter.
		isNew, _ := g.runtime.add(peerKey, nil)
		if isNew {
			fmt.Fprintf(os.Stderr, "wireguard-go: ToNAT NatClient registered (%s…) — add AllowedIPs on B for SNAT\n", hx[:8])
			g.logger.Verbosef("NAT gateway: ToNAT client %s (no AllowedIPs host for MASQUERADE)", hx[:16])
		}
		return
	}
	isNew, newHosts := g.runtime.add(peerKey, hosts)
	if !isNew && len(newHosts) == 0 {
		return
	}
	if isNew {
		fmt.Fprintf(os.Stderr, "wireguard-go: ToNAT NatClient auto-registered %v\n", hosts)
		g.logger.Verbosef("NAT gateway: ToNAT client %s hosts %v", hx[:16], hosts)
	}
	for _, h := range newHosts {
		g.addClientMasquerade(h)
	}
}

// isNestedUpstreamDial reports whether packet is TCP/UDP to upstream IP:port.
func isNestedUpstreamDial(packet []byte, upIP netip.Addr, upPort uint16) bool {
	if len(packet) < 1 {
		return false
	}
	switch packet[0] >> 4 {
	case 4:
		if len(packet) < 20 {
			return false
		}
		ihl := int(packet[0]&0x0f) * 4
		if ihl < 20 || len(packet) < ihl {
			return false
		}
		dst, ok := netip.AddrFromSlice(packet[16:20])
		if !ok || dst.Unmap() != upIP.Unmap() {
			return false
		}
		proto := packet[9]
		if proto != 6 && proto != 17 {
			return false
		}
		if len(packet) < ihl+4 {
			return false
		}
		dport := binary.BigEndian.Uint16(packet[ihl+2 : ihl+4])
		return dport == upPort
	case 6:
		if len(packet) < 40 {
			return false
		}
		dst, ok := netip.AddrFromSlice(packet[24:40])
		if !ok || dst != upIP {
			return false
		}
		proto := packet[6]
		offset := 40
		for proto != 6 && proto != 17 && proto != 59 && offset+8 <= len(packet) {
			if proto == 0 || proto == 43 || proto == 60 {
				hdrLen := int(packet[offset+1]+1) * 8
				proto = packet[offset]
				offset += hdrLen
				continue
			}
			if proto == 44 {
				proto = packet[offset]
				offset += 8
				continue
			}
			break
		}
		if proto != 6 && proto != 17 {
			return false
		}
		if len(packet) < offset+4 {
			return false
		}
		dport := binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		return dport == upPort
	default:
		return false
	}
}
