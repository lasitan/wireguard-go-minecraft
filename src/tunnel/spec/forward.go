/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package spec

import (
	"net"
	"strconv"
)

const (
	ProtoTCP = "tcp"
	ProtoUDP = "udp"
)

type PortForwardSpec struct {
	Proto      string // "tcp" or "udp"
	ListenHost string
	ListenPort int
	DestHost   string
	DestPort   int
}

func (s PortForwardSpec) ListenAddr() string {
	host := s.ListenHost
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.ListenPort))
}

func (s PortForwardSpec) DestAddr() string {
	return net.JoinHostPort(s.DestHost, strconv.Itoa(s.DestPort))
}

func (s PortForwardSpec) String() string {
	return s.Proto + " " + s.ListenAddr() + " -> " + s.DestAddr()
}
