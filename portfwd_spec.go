/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"net"
	"strconv"
)

const (
	protoTCP = "tcp"
	protoUDP = "udp"
)

type portForwardSpec struct {
	Proto      string // "tcp" or "udp"
	ListenHost string
	ListenPort int
	DestHost   string
	DestPort   int
}

func (s portForwardSpec) ListenAddr() string {
	host := s.ListenHost
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.ListenPort))
}

func (s portForwardSpec) DestAddr() string {
	return net.JoinHostPort(s.DestHost, strconv.Itoa(s.DestPort))
}

func (s portForwardSpec) String() string {
	return s.Proto + " " + s.ListenAddr() + " -> " + s.DestAddr()
}
