//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.zx2c4.com/wireguard/device"
)

// natGateway manages IP forwarding (IPEnableRouter / NetIPInterface.Forwarding),
// best-effort WinNAT + firewall nested block, and userspace nested drop on B.
type natGateway struct {
	logger  *device.Logger
	iface   string
	cfg     natGatewayResult
	runtime *natRuntime

	mu                 sync.Mutex
	closed             bool
	prevIPEnableRouter uint32
	changedRouter      bool
	prevForwarding     map[uint32]string
	natName            string
	fwRuleNames        []string
}

func newNatGateway(logger *device.Logger, iface string, cfg natGatewayResult) *natGateway {
	return &natGateway{
		logger:         logger,
		iface:          iface,
		cfg:            cfg,
		runtime:        newNatRuntime(cfg.clientKeyHex),
		prevForwarding: make(map[uint32]string),
		natName:        "wggo-nat-" + sanitizeWinName(iface),
	}
}

func sanitizeWinName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "iface"
	}
	if len(out) > 40 {
		out = out[:40]
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

	for _, h := range g.cfg.clientHosts {
		g.runtime.mu.Lock()
		g.runtime.hosts[h] = struct{}{}
		g.runtime.mu.Unlock()
	}

	if err := g.enableForwarding(); err != nil {
		return err
	}

	if err := g.installFirewall(); err != nil {
		g.logger.Verbosef("NAT gateway: WinNAT/firewall setup warning: %v (userspace nested-block still active)", err)
	}

	installNatInboundFilter(dev, g.runtime, g.cfg.upstreamAddr, g.logger)

	fmt.Fprintf(os.Stderr, "wireguard-go: NAT gateway ready on %s (windows); ToNAT clients auto-register; block nested %s\n",
		g.iface, g.cfg.upstreamAddr.String())
	g.logger.Verbosef("NAT gateway: IP forwarding + dynamic ToNAT NatClient")
	return nil
}

func (g *natGateway) addClientMasquerade(host string) {
	// WinNAT is prefix-based; per-host add is a no-op beyond logging.
	g.logger.Verbosef("NAT gateway: ToNAT client host %s (WinNAT covers VPN prefix)", host)
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

func (g *natGateway) runPS(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return s, fmt.Errorf("powershell timed out")
		}
		return s, fmt.Errorf("%w (%s)", err, s)
	}
	return s, nil
}

// enableForwarding is the Windows equivalent of Linux net.ipv4.ip_forward=1:
//  1. HKLM\...\Tcpip\Parameters\IPEnableRouter = 1
//  2. Set-NetIPInterface -Forwarding Enabled on all interfaces (Win8+/Server2012+)
func (g *natGateway) enableForwarding() error {
	const tcpipParams = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, tcpipParams, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open IPEnableRouter registry key (need Administrator): %w", err)
	}
	defer k.Close()

	if v, _, err := k.GetIntegerValue("IPEnableRouter"); err == nil {
		g.prevIPEnableRouter = uint32(v)
	} else {
		g.prevIPEnableRouter = 0
	}
	if g.prevIPEnableRouter != 1 {
		if err := k.SetDWordValue("IPEnableRouter", 1); err != nil {
			return fmt.Errorf("set IPEnableRouter=1: %w", err)
		}
		g.changedRouter = true
		g.logger.Verbosef("NAT gateway: set IPEnableRouter=1 (was %d)", g.prevIPEnableRouter)
	}

	// Snapshot and enable per-interface forwarding (immediate effect without reboot).
	out, err := g.runPS(`
Get-NetIPInterface | ForEach-Object {
  '{0}|{1}|{2}' -f $_.InterfaceIndex, $_.AddressFamily, $_.Forwarding
}`)
	if err != nil {
		return fmt.Errorf("query NetIPInterface Forwarding: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}
		idx64, err := strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			continue
		}
		idx := uint32(idx64)
		family := parts[1] // IPv4 / IPv6
		state := parts[2]  // Enabled / Disabled
		storeKey := idx
		if family == "IPv6" {
			storeKey = idx | 0x80000000
		}
		g.prevForwarding[storeKey] = state
		if !strings.EqualFold(state, "Enabled") {
			script := fmt.Sprintf(
				`Set-NetIPInterface -InterfaceIndex %d -AddressFamily %s -Forwarding Enabled -ErrorAction Stop`,
				idx, family,
			)
			if _, err := g.runPS(script); err != nil {
				g.logger.Verbosef("NAT gateway: enable Forwarding on ifIndex %d %s: %v", idx, family, err)
			}
		}
	}

	// Also ensure the WG alias has forwarding (by name).
	for _, family := range []string{"IPv4", "IPv6"} {
		script := fmt.Sprintf(
			`Get-NetIPInterface -InterfaceAlias '%s' -AddressFamily %s -ErrorAction SilentlyContinue | Set-NetIPInterface -Forwarding Enabled -ErrorAction SilentlyContinue`,
			psQuote(g.iface), family,
		)
		_, _ = g.runPS(script)
	}

	fmt.Fprintln(os.Stderr, "wireguard-go: enabled Windows IP forwarding (IPEnableRouter + NetIPInterface)")
	return nil
}

func (g *natGateway) restoreForwarding() {
	if g.changedRouter {
		const tcpipParams = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters`
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, tcpipParams, registry.SET_VALUE); err == nil {
			_ = k.SetDWordValue("IPEnableRouter", g.prevIPEnableRouter)
			k.Close()
		}
	}
	for storeKey, state := range g.prevForwarding {
		if strings.EqualFold(state, "Enabled") {
			continue
		}
		idx := storeKey & 0x7fffffff
		family := "IPv4"
		if storeKey&0x80000000 != 0 {
			family = "IPv6"
		}
		script := fmt.Sprintf(
			`Set-NetIPInterface -InterfaceIndex %d -AddressFamily %s -Forwarding %s -ErrorAction SilentlyContinue`,
			idx, family, state,
		)
		_, _ = g.runPS(script)
	}
}

func (g *natGateway) installFirewall() error {
	// WinNAT for SNAT (optional; requires supported SKU).
	var lastErr error
	for _, pfx := range g.cfg.vpnPrefixes {
		if !pfx.IsValid() {
			continue
		}
		script := fmt.Sprintf(`
$name = '%s'
$prefix = '%s'
Get-NetNat -Name $name -ErrorAction SilentlyContinue | Remove-NetNat -Confirm:$false -ErrorAction SilentlyContinue
New-NetNat -Name $name -InternalIPInterfaceAddressPrefix $prefix -ErrorAction Stop | Out-Null
`, psQuote(g.natName), pfx.String())
		if _, err := g.runPS(script); err != nil {
			lastErr = err
			g.logger.Verbosef("NAT gateway: New-NetNat %s: %v", pfx, err)
		} else {
			lastErr = nil
			break
		}
	}

	// Firewall block nested dial to upstream public endpoint.
	upIP := g.cfg.upstreamAddr.Addr().String()
	upPort := int(g.cfg.upstreamAddr.Port())
	for _, proto := range []string{"TCP", "UDP"} {
		rule := fmt.Sprintf("%s-block-%s-%d", g.natName, strings.ToLower(proto), upPort)
		script := fmt.Sprintf(`
$rule = '%s'
Get-NetFirewallRule -DisplayName $rule -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName $rule -Direction Outbound -Action Block -RemoteAddress '%s' -Protocol %s -RemotePort %d -Profile Any -ErrorAction Stop | Out-Null
`, psQuote(rule), upIP, proto, upPort)
		if _, err := g.runPS(script); err != nil {
			g.logger.Verbosef("NAT gateway: firewall rule %s: %v", rule, err)
			if lastErr == nil {
				lastErr = err
			}
		} else {
			g.fwRuleNames = append(g.fwRuleNames, rule)
		}
	}
	return lastErr
}

func (g *natGateway) removeFirewall() {
	if g.natName != "" {
		script := fmt.Sprintf(
			`Get-NetNat -Name '%s' -ErrorAction SilentlyContinue | Remove-NetNat -Confirm:$false -ErrorAction SilentlyContinue`,
			psQuote(g.natName),
		)
		_, _ = g.runPS(script)
	}
	for _, rule := range g.fwRuleNames {
		script := fmt.Sprintf(
			`Get-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue`,
			psQuote(rule),
		)
		_, _ = g.runPS(script)
	}
}
