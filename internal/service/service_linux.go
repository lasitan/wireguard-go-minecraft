//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/meshcfg"
)

const ExitSetupFailed = 1

const (
	defaultIface         = "wg0"
	systemdUnitPathLib   = "/lib/systemd/system/wireguard-go@.service"
	systemdUnitPathEtc   = "/etc/systemd/system/wireguard-go@.service"
	systemdMasterUnitLib = "/lib/systemd/system/wireguard-go-master.service"
	systemdMasterUnitEtc = "/etc/systemd/system/wireguard-go-master.service"
)

func HandleCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "install":
		target := defaultIface
		if len(os.Args) >= 3 {
			target = os.Args[2]
		}
		if len(os.Args) > 3 {
			fmt.Fprintln(os.Stderr, "Usage: wireguard-go install [INTERFACE|master]")
			os.Exit(ExitSetupFailed)
		}
		if target == "master" {
			if err := serviceInstallMaster(); err != nil {
				fmt.Fprintf(os.Stderr, "install master: %v\n", err)
				os.Exit(ExitSetupFailed)
			}
			return true
		}
		if err := serviceInstall(target); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		return true

	case "uninstall":
		target := defaultIface
		purge := false
		for _, a := range os.Args[2:] {
			switch a {
			case "--purge":
				purge = true
			default:
				if strings.HasPrefix(a, "-") {
					fmt.Fprintln(os.Stderr, "Usage: wireguard-go uninstall [INTERFACE|master] [--purge]")
					os.Exit(ExitSetupFailed)
				}
				target = a
			}
		}
		if target == "master" {
			if err := serviceUninstallMaster(purge); err != nil {
				fmt.Fprintf(os.Stderr, "uninstall master: %v\n", err)
				os.Exit(ExitSetupFailed)
			}
			return true
		}
		if err := serviceUninstall(target, purge); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		return true

	default:
		return false
	}
}

func serviceInstall(iface string) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	if err := assertCanInstall(meshcfg.RoleAgent); err != nil {
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
	if err := writeRoleLock(meshcfg.RoleAgent); err != nil {
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

func serviceInstallMaster() error {
	if err := ensureElevated(); err != nil {
		return err
	}
	if err := assertCanInstall(meshcfg.RoleMaster); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if err := os.MkdirAll(meshcfg.ConfDir(), 0755); err != nil {
		return err
	}
	masterCfg := filepath.Join(meshcfg.ConfDir(), "wireguard-go-master.json")
	if _, err := os.Stat(masterCfg); os.IsNotExist(err) {
		example := `{
  "listen": ":8443",
  "adminPassword": "change-me",
  "dataDir": "/var/lib/wireguard-mc"
}
`
		if err := os.WriteFile(masterCfg, []byte(example), 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: created %s — set adminPassword before use\n", masterCfg)
	}
	_ = os.MkdirAll("/var/lib/wireguard-mc", 0750)

	unitPath := systemdMasterUnitLib
	if _, err := os.Stat(unitPath); err != nil {
		unitPath = systemdMasterUnitEtc
		body := systemdMasterUnitTemplate(exe)
		if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(unitPath, []byte(body), 0644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: wrote %s\n", unitPath)
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: using packaged unit %s\n", unitPath)
	}
	if err := writeRoleLock(meshcfg.RoleMaster); err != nil {
		return err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", "--now", "wireguard-go-master"); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wireguard-go: enabled and started wireguard-go-master")
	fmt.Fprintln(os.Stderr, "wireguard-go: this host is locked as master (cannot install agent roles)")
	fmt.Fprintln(os.Stderr, "wireguard-go: status → systemctl status wireguard-go-master")
	return nil
}

func serviceUninstallMaster(purge bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	_ = runSystemctl("disable", "--now", "wireguard-go-master")
	_ = clearRoleLock()
	if purge {
		_ = os.Remove(filepath.Join(meshcfg.ConfDir(), "wireguard-go-master.json"))
		if err := os.Remove(systemdMasterUnitEtc); err == nil {
			_ = runSystemctl("daemon-reload")
		}
	}
	fmt.Fprintln(os.Stderr, "wireguard-go: uninstalled master service")
	return nil
}

func serviceUninstall(iface string, purge bool) error {
	if err := ensureElevated(); err != nil {
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
	_ = clearRoleLock()

	if purge {
		conf := filepath.Join(meshcfg.ConfDir(), iface+".conf")
		if err := os.Remove(conf); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", conf)
		}
		transport := filepath.Join(meshcfg.ConfDir(), "wireguard-go-transport.json")
		if err := os.Remove(transport); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", transport)
		}
		agent := filepath.Join(meshcfg.ConfDir(), "wireguard-go-agent.json")
		if err := os.Remove(agent); err == nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: removed %s\n", agent)
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: kept configs under %s (pass --purge to delete)\n", meshcfg.ConfDir())
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

func systemdMasterUnitTemplate(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=wireguard-go Master (mesh control plane)
Documentation=file:///usr/share/doc/wireguard-mc/README.Debian
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=LOG_LEVEL=error
ExecStart=%s master
Restart=on-failure
RestartSec=2
TimeoutStopSec=10
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, exe)
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
	if err := os.MkdirAll(meshcfg.ConfDir(), 0755); err != nil {
		return err
	}
	agentPath := filepath.Join(meshcfg.ConfDir(), "wireguard-go-agent.json")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		example := `{
  "masterUrl": "http://127.0.0.1:8443",
  "nodeId": "CHANGE_ME",
  "nodeToken": "CHANGE_ME",
  "pollInterval": "10s",
  "interface": "` + iface + `"
}
`
		if err := os.WriteFile(agentPath, []byte(example), 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go: created %s — set masterUrl/nodeId/nodeToken\n", agentPath)
	}
	transport := filepath.Join(meshcfg.ConfDir(), "wireguard-go-transport.json")
	if _, err := os.Stat(transport); os.IsNotExist(err) {
		example := filepath.Join(meshcfg.ConfDir(), "wireguard-go-transport.json.example")
		if data, readErr := os.ReadFile(example); readErr == nil {
			_ = os.WriteFile(transport, data, 0644)
			fmt.Fprintf(os.Stderr, "wireguard-go: created %s from example\n", transport)
		}
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
