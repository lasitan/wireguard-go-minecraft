//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

// natGateway manages IP forwarding, MASQUERADE, and nested-WG hard block on B.
type natGateway struct {
	logger *device.Logger
	iface  string
	cfg    natGatewayResult
	runtime *natRuntime

	mu       sync.Mutex
	closed   bool
	backend  string // "nft" or "iptables"
	nftTable string
	// iptables rules we added (for teardown), each is args after -t <table> or filter.
	iptNatRules     [][]string
	iptFilterRules  [][]string
	prevIPv4Forward string
	prevIPv6Forward string
	changedIPv4     bool
	changedIPv6     bool
}

func newNatGateway(logger *device.Logger, iface string, cfg natGatewayResult) *natGateway {
	return &natGateway{
		logger:   logger,
		iface:    iface,
		cfg:      cfg,
		runtime:  newNatRuntime(cfg.clientKeyHex),
		nftTable: "wggo_nat_" + sanitizeNftName(iface),
	}
}

func sanitizeNftName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "iface"
	}
	return out
}

func (g *natGateway) Start(dev *device.Device) error {
	if !g.cfg.serverMode {
		return nil
	}
	if !g.cfg.upstreamAddr.IsValid() {
		return fmt.Errorf("nat gateway: missing upstream endpoint")
	}

	// Seed hosts from explicit NatClient=true
	for _, h := range g.cfg.clientHosts {
		g.runtime.mu.Lock()
		g.runtime.hosts[h] = struct{}{}
		g.runtime.mu.Unlock()
	}

	if err := g.enableForwarding(); err != nil {
		return err
	}

	if err := g.installFirewall(); err != nil {
		g.restoreForwarding()
		return err
	}

	installNatInboundFilter(dev, g.runtime, g.cfg.upstreamAddr, g.logger)

	fmt.Fprintf(os.Stderr, "wireguard-go: NAT gateway ready on %s (%s); ToNAT clients auto-register; block nested %s\n",
		g.iface, g.backend, g.cfg.upstreamAddr.String())
	g.logger.Verbosef("NAT gateway: forward+SNAT+nested-block via %s (dynamic ToNAT)", g.backend)
	return nil
}

func (g *natGateway) Close(dev *device.Device) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	if dev != nil {
		dev.SetInboundPacketFilter(nil)
	}
	g.removeFirewall()
	g.restoreForwarding()
}

func (g *natGateway) enableForwarding() error {
	v4, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err == nil {
		g.prevIPv4Forward = strings.TrimSpace(string(v4))
		if g.prevIPv4Forward != "1" {
			if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644); err != nil {
				return fmt.Errorf("enable ipv4 forwarding: %w", err)
			}
			g.changedIPv4 = true
		}
	}
	v6path := "/proc/sys/net/ipv6/conf/all/forwarding"
	v6, err := os.ReadFile(v6path)
	if err == nil {
		g.prevIPv6Forward = strings.TrimSpace(string(v6))
		if g.prevIPv6Forward != "1" {
			if err := os.WriteFile(v6path, []byte("1\n"), 0o644); err != nil {
				g.logger.Verbosef("NAT gateway: ipv6 forwarding not enabled: %v", err)
			} else {
				g.changedIPv6 = true
			}
		}
	}
	return nil
}

func (g *natGateway) restoreForwarding() {
	if g.changedIPv4 && g.prevIPv4Forward != "" {
		_ = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte(g.prevIPv4Forward+"\n"), 0o644)
	}
	if g.changedIPv6 && g.prevIPv6Forward != "" {
		_ = os.WriteFile("/proc/sys/net/ipv6/conf/all/forwarding", []byte(g.prevIPv6Forward+"\n"), 0o644)
	}
}

func (g *natGateway) installFirewall() error {
	if err := g.installNft(); err == nil {
		g.backend = "nft"
		return nil
	} else {
		g.logger.Verbosef("NAT gateway: nft failed (%v), trying iptables", err)
	}
	if err := g.installIptables(); err != nil {
		return fmt.Errorf("firewall setup failed (nft and iptables): %w", err)
	}
	g.backend = "iptables"
	return nil
}

func (g *natGateway) removeFirewall() {
	switch g.backend {
	case "nft":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "nft", "delete", "table", "inet", g.nftTable).Run()
	case "iptables":
		for i := len(g.iptNatRules) - 1; i >= 0; i-- {
			args := append([]string{"-t", "nat", "-D"}, g.iptNatRules[i]...)
			_ = runCmd("iptables", args...)
		}
		for i := len(g.iptFilterRules) - 1; i >= 0; i-- {
			args := append([]string{"-D"}, g.iptFilterRules[i]...)
			_ = runCmd("iptables", args...)
		}
		// IPv6 counterparts if any were added with ip6tables — tracked same lists with marker
		for i := len(g.iptNatRules) - 1; i >= 0; i-- {
			args := append([]string{"-t", "nat", "-D"}, g.iptNatRules[i]...)
			_ = runCmd("ip6tables", args...)
		}
		for i := len(g.iptFilterRules) - 1; i >= 0; i-- {
			args := append([]string{"-D"}, g.iptFilterRules[i]...)
			_ = runCmd("ip6tables", args...)
		}
	}
}

func runCmd(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w (%s)", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (g *natGateway) installNft() error {
	_ = runCmd("nft", "delete", "table", "inet", g.nftTable)

	upIP := g.cfg.upstreamAddr.Addr().String()
	upPort := strconv.Itoa(int(g.cfg.upstreamAddr.Port()))
	up := g.cfg.upstreamAddr.Addr()

	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s {\n", g.nftTable)
	b.WriteString("  chain forward {\n")
	b.WriteString("    type filter hook forward priority 0; policy accept;\n")
	// Block nested dial to public WG port from anything forwarded off the WG iface.
	if up.Is4() {
		fmt.Fprintf(&b, "    iifname \"%s\" ip daddr %s tcp dport %s reject\n", g.iface, upIP, upPort)
		fmt.Fprintf(&b, "    iifname \"%s\" ip daddr %s udp dport %s reject\n", g.iface, upIP, upPort)
	} else {
		fmt.Fprintf(&b, "    iifname \"%s\" ip6 daddr %s tcp dport %s reject\n", g.iface, upIP, upPort)
		fmt.Fprintf(&b, "    iifname \"%s\" ip6 daddr %s udp dport %s reject\n", g.iface, upIP, upPort)
	}
	b.WriteString("  }\n")
	b.WriteString("  chain postrouting {\n")
	b.WriteString("    type nat hook postrouting priority 100; policy accept;\n")
	for _, host := range g.cfg.clientHosts {
		writeNftMasquerade(&b, g.iface, host)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft -f: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeNftMasquerade(b *strings.Builder, iface, host string) {
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return
	}
	if ip.Is4() {
		fmt.Fprintf(b, "    oifname != \"%s\" ip saddr %s masquerade\n", iface, ip)
	} else {
		fmt.Fprintf(b, "    oifname != \"%s\" ip6 saddr %s masquerade\n", iface, ip)
	}
}

func (g *natGateway) addClientMasquerade(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return
	}
	switch g.backend {
	case "nft":
		var args []string
		if ip.Is4() {
			args = []string{"add", "rule", "inet", g.nftTable, "postrouting",
				"oifname", "!=", g.iface, "ip", "saddr", ip.String(), "masquerade"}
		} else {
			args = []string{"add", "rule", "inet", g.nftTable, "postrouting",
				"oifname", "!=", g.iface, "ip6", "saddr", ip.String(), "masquerade"}
		}
		_ = runCmd("nft", args...)
	default:
		bin := "iptables"
		if ip.Is6() {
			bin = "ip6tables"
		}
		rule := []string{"POSTROUTING", "-s", ip.String(), "!", "-o", g.iface, "-j", "MASQUERADE"}
		if err := runCmd(bin, append([]string{"-t", "nat", "-A"}, rule...)...); err == nil {
			g.iptNatRules = append(g.iptNatRules, rule)
		}
	}
}

func (g *natGateway) installIptables() error {
	upIP := g.cfg.upstreamAddr.Addr().String()
	upPort := strconv.Itoa(int(g.cfg.upstreamAddr.Port()))
	upIs4 := g.cfg.upstreamAddr.Addr().Is4()
	bin := "iptables"
	if !upIs4 {
		bin = "ip6tables"
	}
	for _, proto := range []string{"tcp", "udp"} {
		rule := []string{"FORWARD", "-i", g.iface, "-d", upIP, "-p", proto, "--dport", upPort, "-j", "REJECT"}
		if err := runCmd(bin, append([]string{"-I"}, rule...)...); err != nil {
			g.removeFirewall()
			return err
		}
		g.iptFilterRules = append(g.iptFilterRules, rule)
	}
	for _, host := range g.cfg.clientHosts {
		ip, err := netip.ParseAddr(host)
		if err != nil {
			continue
		}
		hbin := "iptables"
		if ip.Is6() {
			hbin = "ip6tables"
		}
		rule := []string{"POSTROUTING", "-s", ip.String(), "!", "-o", g.iface, "-j", "MASQUERADE"}
		if err := runCmd(hbin, append([]string{"-t", "nat", "-A"}, rule...)...); err != nil {
			g.removeFirewall()
			return err
		}
		g.iptNatRules = append(g.iptNatRules, rule)
	}
	return nil
}
