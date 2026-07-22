//go:build windows

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
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

func applyIfaceNetConfig(iface string, cfg ifaceNetConfig, logger *device.Logger) error {
	const cmdTimeout = 15 * time.Second
	runPS := func(script string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
		out, err := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return s, fmt.Errorf("powershell timed out after %s", cmdTimeout)
			}
			return s, fmt.Errorf("%w (%s)", err, s)
		}
		return s, nil
	}

	// Bring interface up (Wintun is usually up after CreateTUN).
	if _, err := runPS(fmt.Sprintf(
		`Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue | Enable-NetAdapter -Confirm:$false -ErrorAction SilentlyContinue`,
		psQuote(iface),
	)); err != nil {
		logger.Verbosef("Enable-NetAdapter %s: %v", iface, err)
	}

	if cfg.mtu > 0 {
		script := fmt.Sprintf(
			`Get-NetIPInterface -InterfaceAlias '%s' -ErrorAction Stop | ForEach-Object { Set-NetIPInterface -InterfaceIndex $_.InterfaceIndex -NlMtuBytes %d -ErrorAction Stop }`,
			psQuote(iface), cfg.mtu,
		)
		if _, err := runPS(script); err != nil {
			return fmt.Errorf("set mtu %d on %s: %w", cfg.mtu, iface, err)
		}
		logger.Verbosef("Set %s mtu %d", iface, cfg.mtu)
	}

	for _, addr := range cfg.addresses {
		prefix, err := netip.ParsePrefix(addr)
		if err != nil {
			ip, err2 := netip.ParseAddr(addr)
			if err2 != nil {
				return fmt.Errorf("invalid Address %q: %w", addr, err)
			}
			if ip.Is4() {
				prefix = netip.PrefixFrom(ip, 32)
			} else {
				prefix = netip.PrefixFrom(ip, 128)
			}
		}
		family := "IPv4"
		if prefix.Addr().Is6() {
			family = "IPv6"
		}
		// Remove existing addresses of same family then add.
		script := fmt.Sprintf(`
$ifAlias = '%s'
$ip = '%s'
$prefix = %d
Get-NetIPAddress -InterfaceAlias $ifAlias -AddressFamily %s -ErrorAction SilentlyContinue |
  Where-Object { $_.IPAddress -eq $ip } |
  Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue
New-NetIPAddress -InterfaceAlias $ifAlias -IPAddress $ip -PrefixLength $prefix -AddressFamily %s -PolicyStore ActiveStore -ErrorAction Stop | Out-Null
`, psQuote(iface), prefix.Addr().String(), prefix.Bits(), family, family)
		if _, err := runPS(script); err != nil {
			return fmt.Errorf("addr %s on %s: %w", addr, iface, err)
		}
		logger.Verbosef("Assigned %s to %s", addr, iface)
		fmt.Fprintf(os.Stderr, "wireguard-go: assigned %s to %s\n", addr, iface)
	}
	return nil
}

func psQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}