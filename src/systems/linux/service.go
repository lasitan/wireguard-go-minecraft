//go:build linux

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/src/commands/role"
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/update"

	"golang.zx2c4.com/wireguard/src/core/config"
)

const (
	defaultIface         = config.DefaultIface
	systemdUnitPathLib   = "/lib/systemd/system/lasitan-cluster@.service"
	systemdUnitPathEtc   = "/etc/systemd/system/lasitan-cluster@.service"
	systemdMasterUnitLib = "/lib/systemd/system/lasitan-cluster-master.service"
	systemdMasterUnitEtc = "/etc/systemd/system/lasitan-cluster-master.service"
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
			fmt.Fprintln(os.Stderr, "Usage: lasitan-cluster install [INTERFACE|master]")
			os.Exit(core.ExitSetupFailed)
		}
		if target == "master" {
			if err := serviceInstallMaster(); err != nil {
				fmt.Fprintf(os.Stderr, "install master: %v\n", err)
				os.Exit(core.ExitSetupFailed)
			}
			return true
		}
		if err := serviceInstall(target); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(core.ExitSetupFailed)
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
					fmt.Fprintln(os.Stderr, "Usage: lasitan-cluster uninstall [INTERFACE|master] [--purge]")
					os.Exit(core.ExitSetupFailed)
				}
				target = a
			}
		}
		if target == "master" {
			if err := serviceUninstallMaster(purge); err != nil {
				fmt.Fprintf(os.Stderr, "uninstall master: %v\n", err)
				os.Exit(core.ExitSetupFailed)
			}
			return true
		}
		if err := serviceUninstall(target, purge); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(core.ExitSetupFailed)
		}
		return true

	case "update":
		// Best-effort: finish any interrupted wireguard-go → lasitan-cluster migration.
		_ = MigrateLegacy(false)
		if err := update.RunCommand(os.Args[2:], ensureElevated, applyUpdate); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(core.ExitSetupFailed)
		}
		_ = MigrateLegacy(true)
		return true

	case "migrate":
		start := true
		for _, a := range os.Args[2:] {
			switch a {
			case "--no-start":
				start = false
			case "--start":
				start = true
			case "-h", "--help":
				fmt.Fprintln(os.Stderr, "Usage: lasitan-cluster migrate [--no-start|--start]")
				fmt.Fprintln(os.Stderr, "  检测并迁移旧版 wireguard-go / wireguard-mc（配置、systemd、二进制）到 lasitan-cluster")
				return true
			default:
				fmt.Fprintf(os.Stderr, "unknown flag %q\nUsage: lasitan-cluster migrate [--no-start|--start]\n", a)
				os.Exit(core.ExitSetupFailed)
			}
		}
		if err := MigrateLegacy(start); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
			os.Exit(core.ExitSetupFailed)
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
	_ = MigrateLegacy(false)
	if err := role.AssertCanInstall(config.RoleAgent); err != nil {
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
		fmt.Fprintf(os.Stderr, "lasitan-cluster: using packaged unit %s\n", systemdUnitPathLib)
	} else {
		unitBody := systemdUnitTemplate(exe)
		if err := os.MkdirAll(filepath.Dir(systemdUnitPathEtc), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(systemdUnitPathEtc, []byte(unitBody), 0644); err != nil {
			return fmt.Errorf("write %s: %w", systemdUnitPathEtc, err)
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: wrote %s\n", systemdUnitPathEtc)
	}

	if err := ensureWGConfigs(iface); err != nil {
		return err
	}
	if err := role.Write(config.RoleAgent); err != nil {
		return err
	}

	unitInstance := fmt.Sprintf("lasitan-cluster@%s", iface)
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", "--now", unitInstance); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: enabled and started %s\n", unitInstance)
	fmt.Fprintf(os.Stderr, "lasitan-cluster: status → systemctl status %s\n", unitInstance)
	fmt.Fprintf(os.Stderr, "lasitan-cluster: logs   → journalctl -u %s -f\n", unitInstance)
	return nil
}

func serviceInstallMaster() error {
	if err := ensureElevated(); err != nil {
		return err
	}
	_ = MigrateLegacy(false)
	if err := role.AssertCanInstall(config.RoleMaster); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if err := os.MkdirAll(config.ConfDir(), 0755); err != nil {
		return err
	}
	masterCfg := filepath.Join(config.ConfDir(), "lasitan-cluster-master.json")
	if _, err := os.Stat(masterCfg); os.IsNotExist(err) {
		example := `{
  "listen": ":8443",
  "adminPassword": "change-me",
  "dataDir": "/var/lib/lasitan-cluster"
}
`
		if err := os.WriteFile(masterCfg, []byte(example), 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: created %s — set adminPassword before use; enroll key is managed in the web UI\n", masterCfg)
	}
	_ = os.MkdirAll("/var/lib/lasitan-cluster", 0750)

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
		fmt.Fprintf(os.Stderr, "lasitan-cluster: wrote %s\n", unitPath)
	} else {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: using packaged unit %s\n", unitPath)
	}
	if err := role.Write(config.RoleMaster); err != nil {
		return err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", "--now", "lasitan-cluster-master"); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "lasitan-cluster: enabled and started lasitan-cluster-master")
	fmt.Fprintln(os.Stderr, "lasitan-cluster: this host is locked as master (cannot install agent roles)")
	fmt.Fprintln(os.Stderr, "lasitan-cluster: status → systemctl status lasitan-cluster-master")
	return nil
}

func serviceUninstallMaster(purge bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	_ = runSystemctl("disable", "--now", "lasitan-cluster-master")
	_ = role.Clear()
	if purge {
		_ = os.Remove(filepath.Join(config.ConfDir(), "lasitan-cluster-master.json"))
		if err := os.Remove(systemdMasterUnitEtc); err == nil {
			_ = runSystemctl("daemon-reload")
		}
	}
	fmt.Fprintln(os.Stderr, "lasitan-cluster: uninstalled master service")
	return nil
}

func serviceUninstall(iface string, purge bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	if err := validateIfaceName(iface); err != nil {
		return err
	}

	unitInstance := fmt.Sprintf("lasitan-cluster@%s", iface)
	_ = runSystemctl("disable", "--now", unitInstance)

	// Best-effort: stop any leftover non-systemd process and remove TUN.
	_ = exec.Command("pkill", "-x", "lasitan-cluster").Run()
	if out, err := exec.Command("ip", "link", "show", iface).CombinedOutput(); err == nil && len(out) > 0 {
		_ = exec.Command("ip", "link", "set", "dev", iface, "down").Run()
		_ = exec.Command("ip", "link", "delete", "dev", iface).Run()
		fmt.Fprintf(os.Stderr, "lasitan-cluster: removed interface %s\n", iface)
	}
	_ = os.Remove(filepath.Join("/var/run/lasitan-cluster", iface+".sock"))
	_ = role.Clear()

	if purge {
		conf := filepath.Join(config.ConfDir(), iface+".conf")
		if err := os.Remove(conf); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: removed %s\n", conf)
		}
		transport := filepath.Join(config.ConfDir(), "lasitan-cluster-transport.json")
		if err := os.Remove(transport); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: removed %s\n", transport)
		}
		agent := filepath.Join(config.ConfDir(), "lasitan-cluster-agent.json")
		if err := os.Remove(agent); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: removed %s\n", agent)
		}
	} else {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: kept configs under %s (pass --purge to delete)\n", config.ConfDir())
	}

	if purge {
		if err := os.Remove(systemdUnitPathEtc); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: removed %s\n", systemdUnitPathEtc)
			_ = runSystemctl("daemon-reload")
		}
	}

	fmt.Fprintf(os.Stderr, "lasitan-cluster: uninstalled service %s\n", unitInstance)
	return nil
}

func systemdMasterUnitTemplate(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=Lasitan-Cluster Master (mesh control plane)
Documentation=file:///usr/share/doc/lasitan-cluster/README.Debian
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
Description=Lasitan-Cluster tunnel (%%i)
Documentation=file:///usr/share/doc/lasitan-cluster/README.Debian
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
	if err := os.MkdirAll(config.ConfDir(), 0755); err != nil {
		return err
	}
	agentPath := filepath.Join(config.ConfDir(), "lasitan-cluster-agent.json")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		example := `{
  "masterUrl": "http://127.0.0.1:8443",
  "key": "CHANGE_ME"
}
`
		if err := os.WriteFile(agentPath, []byte(example), 0600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: created %s — set masterUrl/key only\n", agentPath)
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
