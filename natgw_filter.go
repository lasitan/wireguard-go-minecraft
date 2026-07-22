/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"encoding/binary"
	"encoding/hex"
	"net/netip"

	"golang.zx2c4.com/wireguard/device"
)

func installNatInboundFilter(dev *device.Device, cfg natGatewayResult, logger *device.Logger) {
	clientKeys := make(map[device.NoisePublicKey]struct{}, len(cfg.clientKeyHex))
	for _, hx := range cfg.clientKeyHex {
		var pk device.NoisePublicKey
		raw, err := hex.DecodeString(hx)
		if err != nil || len(raw) != device.NoisePublicKeySize {
			continue
		}
		copy(pk[:], raw)
		clientKeys[pk] = struct{}{}
	}
	upIP := cfg.upstreamAddr.Addr()
	upPort := cfg.upstreamAddr.Port()
	upStr := cfg.upstreamAddr.String()

	dev.SetInboundPacketFilter(func(peerKey device.NoisePublicKey, packet []byte) bool {
		if _, ok := clientKeys[peerKey]; !ok {
			return true
		}
		if isNestedUpstreamDial(packet, upIP, upPort) {
			logger.Verbosef("NAT gateway: dropped nested dial to upstream %s from NatClient", upStr)
			return false
		}
		return true
	})
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
