//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultIface       = "wg0"
	systemdUnitPathLib = "/lib/systemd/system/wireguard-go@.service"
	systemdUnitPathEtc = "/etc/systemd/system/wireguard-go@.service"
	wgConfDirInstall   = "/etc/wireguard"
)

func handleServiceCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "install":
		iface := defaultIface
		if len(os.Args) >= 3 {
			iface = os.Args[2]
		}
		if len(os.Args) > 3 {
			fmt.Fprintln(os.Stderr, "Usage: wireguard-go install [INTERFACE]")
			os.Exit(ExitSetupFailed)
		}
		if err := serviceInstall(iface); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		return true

	case "uninstall":
		iface := defaultIface
		purge := false
		for _, a := range os.Args[2:] {
			switch a {
			case "--purge":
				purge = true
			default:
				if strings.HasPrefix(a, "-") {
					fmt.Fprintln(os.Stderr, "Usage: wireguard-go uninstall [INTERFACE] [--purge]")
					os.Exit(ExitSetupFailed)
				}
				iface = a
			}
		}
		if err := serviceUninstall(iface, purge); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		return true

	default:
		return false
	}
}

func serviceInstall(iface string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if err := validateIfaceName(iface); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	if _, err := os.Stat(systemdUnitPathLib); err == nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: using packaged unit %s\n", systemdUnitPathLib)
	} else {
		unitBody := systemdUnitTemplate(exe)
		if err := os.MkdirAll(filepath.Dir(systemdUnitPathEtc), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(systemdUnitPathEtc, []byte(unitBody), 0644); err != nil {
			return fmt.Errorf("write %s: %w", systemdUnitPathEtc, err)
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: wrote %s\n", systemdUnitPathEtc)
	}

	if err := ensureWGConfigs(iface); err != nil {
		return err
	}

	unitInstance := fmt.Sprintf("wireguard-go@%s", iface)
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", "--now", unitInstance); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: enabled and started %s\n", unitInstance)
	fmt.Fprintf(os.Stderr, "wireguard-go: status → systemctl status %s\n", unitInstance)
	fmt.Fprintf(os.Stderr, "wireguard-go: logs   → journalctl -u %s -f\n", unitInstance)
	return nil
}

func serviceUninstall(iface string, purge bool) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if err := validateIfaceName(iface); err != nil {
		return err
	}

	unitInstance := fmt.Sprintf("wireguard-go@%s", iface)
	_ = runSystemctl("disable", "--now", unitInstance)

	// Best-effort: stop any leftover non-systemd process and remove TUN.
	_ = exec.Command("pkill", "-x", "wireguard-go").Run()
	if out, err := exec.Command("ip", "link", "show", iface).CombinedOutput(); err == nil && len(out) > 0 {
		_ = exec.Command("ip", "link", "set", "dev", iface, "down").Run()
		_ = exec.Command("ip", "link", "delete", "dev", iface).Run()
		fmt.Fprintf(os.Stderr, "wireguard-go: removed interface %s\n", iface)
	}
	_ = os.Remove(filepath.Join("/var/run/wireguard", iface+".sock"))

	if purge {
		conf := filepath.Join(wgConfDirInstall, iface+".conf")
		if err := os.Remove(conf); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", conf)
		}
		transport := filepath.Join(wgConfDirInstall, "wireguard-go-transport.json")
		if err := os.Remove(transport); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", transport)
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: kept configs under %s (pass --purge to delete)\n", wgConfDirInstall)
	}

	if purge {
		if err := os.Remove(systemdUnitPathEtc); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", systemdUnitPathEtc)
			_ = runSystemctl("daemon-reload")
		}
	}

	fmt.Fprintf(os.Stderr, "wireguard-go: uninstalled service %s\n", unitInstance)
	return nil
}

func systemdUnitTemplate(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=wireguard-go TCP+MC tunnel (%%i)
Documentation=file:///usr/share/doc/wireguard-mc/README.Debian
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=LOG_LEVEL=error
ExecStart=%s -f %%i
Restart=on-failure
RestartSec=2
TimeoutStopSec=10

# TUN / binding privileged ports
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, exe)
}

func ensureWGConfigs(iface string) error {
	if err := os.MkdirAll(wgConfDirInstall, 0755); err != nil {
		return err
	}
	confPath := filepath.Join(wgConfDirInstall, iface+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		example := filepath.Join(wgConfDirInstall, "wg0.conf.example")
		if iface != "wg0" {
			// still prefer generic example if present
			if _, e := os.Stat(example); e != nil {
				example = filepath.Join(wgConfDirInstall, iface+".conf.example")
			}
		}
		data, readErr := os.ReadFile(example)
		if readErr != nil {
			return fmt.Errorf("%s missing — create it (see %s/wg0.conf.example)", confPath, wgConfDirInstall)
		}
		if err := os.WriteFile(confPath, data, 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: created %s from example — edit keys before relying on the tunnel\n", confPath)
	}
	transport := filepath.Join(wgConfDirInstall, "wireguard-go-transport.json")
	if _, err := os.Stat(transport); os.IsNotExist(err) {
		example := filepath.Join(wgConfDirInstall, "wireguard-go-transport.json.example")
		if data, readErr := os.ReadFile(example); readErr == nil {
			_ = os.WriteFile(transport, data, 0644)
			fmt.Fprintf(os.Stderr, "wireguard-go: created %s from example\n", transport)
		}
	}
	return nil
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root (try: sudo %s %s)", os.Args[0], strings.Join(os.Args[1:], " "))
	}
	return nil
}

func validateIfaceName(iface string) error {
	if iface == "" || strings.ContainsAny(iface, "/\\ \t\n") || strings.Contains(iface, "..") {
		return fmt.Errorf("invalid interface name %q", iface)
	}
	return nil
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
