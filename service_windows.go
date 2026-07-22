//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	defaultIface         = "wg0"
	windowsServicePrefix = "wireguard-go-"
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

func windowsServiceName(iface string) string {
	return windowsServicePrefix + iface
}

func serviceInstall(iface string) error {
	if err := ensureElevated(); err != nil {
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

	if err := ensureWGConfigsWindows(iface); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	name := windowsServiceName(iface)
	if s, err := m.OpenService(name); err == nil {
		s.Close()
		fmt.Fprintf(os.Stderr, "wireguard-go: service %s already exists — starting\n", name)
		return startWindowsService(m, name)
	}

	cfg := mgr.Config{
		DisplayName:      fmt.Sprintf("wireguard-go (%s)", iface),
		Description:      "WireGuard over TCP with Minecraft camouflage",
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "", // LocalSystem
	}
	s, err := m.CreateService(name, exe, cfg, "-service", iface)
	if err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}
	defer s.Close()

	fmt.Fprintf(os.Stderr, "wireguard-go: created Windows service %s\n", name)
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service %s: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: started %s\n", name)
	fmt.Fprintf(os.Stderr, "wireguard-go: config → %s\n", filepath.Join(wgConfDir(), iface+".conf"))
	return nil
}

func startWindowsService(m *mgr.Mgr, name string) error {
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()
	status, err := s.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Running {
		fmt.Fprintf(os.Stderr, "wireguard-go: %s already running\n", name)
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: started %s\n", name)
	return nil
}

func serviceUninstall(iface string, purge bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	if err := validateIfaceName(iface); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	name := windowsServiceName(iface)
	s, err := m.OpenService(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: service %s not found\n", name)
	} else {
		_, _ = s.Control(svc.Stop)
		// Wait briefly for stop.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			st, err := s.Query()
			if err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err := s.Delete(); err != nil {
			s.Close()
			return fmt.Errorf("delete service %s: %w", name, err)
		}
		s.Close()
		fmt.Fprintf(os.Stderr, "wireguard-go: removed service %s\n", name)
	}

	if purge {
		conf := filepath.Join(wgConfDir(), iface+".conf")
		if err := os.Remove(conf); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", conf)
		}
		transport := filepath.Join(wgConfDir(), "wireguard-go-transport.json")
		if err := os.Remove(transport); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", transport)
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: kept configs under %s (pass --purge to delete)\n", wgConfDir())
	}
	return nil
}

func ensureWGConfigsWindows(iface string) error {
	dir := wgConfDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	confPath := filepath.Join(dir, iface+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		// Create a minimal placeholder so the service has something to load.
		placeholder := fmt.Sprintf(`# Created by wireguard-go install — edit before use
[Interface]
PrivateKey = REPLACE_WITH_PRIVATE_KEY_BASE64
Address = 10.0.0.1/24
ListenPort = 25565
MTU = 1420

# [Peer]
# PublicKey = REPLACE_WITH_PEER_PUBLIC_KEY_BASE64
# AllowedIPs = 10.0.0.2/32
`)
		if err := os.WriteFile(confPath, []byte(placeholder), 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: created %s — edit keys before relying on the tunnel\n", confPath)
	}
	return nil
}

func validateIfaceName(iface string) error {
	if iface == "" || strings.ContainsAny(iface, "/\\ \t\n") || strings.Contains(iface, "..") {
		return fmt.Errorf("invalid interface name %q", iface)
	}
	return nil
}
