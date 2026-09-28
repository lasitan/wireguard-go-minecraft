//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/commands/role"
	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/update"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"golang.zx2c4.com/wireguard/src/core/config"
)

const (
	defaultIface         = config.DefaultIface
	windowsServicePrefix = "lasitan-cluster-"
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
		if err := update.RunCommand(os.Args[2:], ensureElevated, applyUpdate); err != nil {
			fmt.Fprintf(os.Stderr, "update: %v\n", err)
			os.Exit(core.ExitSetupFailed)
		}
		return true

	default:
		return false
	}
}

func WindowsServiceName(iface string) string {
	return windowsServicePrefix + iface
}

func serviceInstall(iface string) error {
	if err := ensureElevated(); err != nil {
		return err
	}
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

	if err := ensureWGConfigsWindows(iface); err != nil {
		return err
	}
	if err := role.Write(config.RoleAgent); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	name := WindowsServiceName(iface)
	if s, err := m.OpenService(name); err == nil {
		defer s.Close()
		if err := updateWindowsServiceBinary(s, exe, iface); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: service %s already exists — refreshed binary path\n", name)
		fmt.Fprintf(os.Stderr, "lasitan-cluster: binary → %s\n", exe)
		return startWindowsServiceHandle(s, name)
	}

	cfg := mgr.Config{
		DisplayName:      fmt.Sprintf("Lasitan-Cluster (%s)", iface),
		Description:      "Lasitan-Cluster agent tunnel (WireGuard over TCP + MC camouflage)",
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "", // LocalSystem
	}
	s, err := m.CreateService(name, exe, cfg, "-service", iface)
	if err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}
	defer s.Close()

	fmt.Fprintf(os.Stderr, "lasitan-cluster: created Windows service %s\n", name)
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service %s: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: started %s\n", name)
	fmt.Fprintf(os.Stderr, "lasitan-cluster: agent config → %s\n", filepath.Join(config.ConfDir(), config.AgentFileName))
	return nil
}

func serviceInstallMaster() error {
	if err := ensureElevated(); err != nil {
		return err
	}
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
	dir := config.ConfDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	masterCfg := filepath.Join(dir, config.MasterFileName)
	if _, err := os.Stat(masterCfg); os.IsNotExist(err) {
		dataDir := filepath.Join(dir, "master-data")
		example := fmt.Sprintf(`{
  "listen": ":8443",
  "adminPassword": "change-me",
  "dataDir": %q
}
`, dataDir)
		if err := os.WriteFile(masterCfg, []byte(example), 0600); err != nil {
			return err
		}
		_ = os.MkdirAll(dataDir, 0750)
		fmt.Fprintf(os.Stderr, "lasitan-cluster: created %s — set adminPassword; enroll key is managed in the web UI\n", masterCfg)
	}
	if err := role.Write(config.RoleMaster); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	name := "lasitan-cluster-master"
	if s, err := m.OpenService(name); err == nil {
		defer s.Close()
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		cfg.BinaryPathName = exe + " master"
		if strings.ContainsAny(exe, " \t") {
			cfg.BinaryPathName = fmt.Sprintf(`"%s" master`, exe)
		}
		_ = s.UpdateConfig(cfg)
		return startWindowsServiceHandle(s, name)
	}
	cfg := mgr.Config{
		DisplayName: "Lasitan-Cluster Master",
		Description: "Lasitan-Cluster mesh control plane",
		StartType:   mgr.StartAutomatic,
	}
	s, err := m.CreateService(name, exe, cfg, "master")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	if err := s.Start(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "lasitan-cluster: started lasitan-cluster-master (host locked as master)")
	return nil
}

func serviceUninstallMaster(purge bool) error {
	if err := ensureElevated(); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	name := "lasitan-cluster-master"
	if s, err := m.OpenService(name); err == nil {
		_, _ = s.Control(svc.Stop)
		time.Sleep(500 * time.Millisecond)
		_ = s.Delete()
		s.Close()
	}
	_ = role.Clear()
	if purge {
		_ = os.Remove(filepath.Join(config.ConfDir(), config.MasterFileName))
	}
	fmt.Fprintln(os.Stderr, "lasitan-cluster: uninstalled master service")
	return nil
}

func windowsServiceBinaryPath(exe, iface string) string {
	// SCM needs a quoted path when spaces exist; CreateService does this for us,
	// UpdateConfig needs the same form.
	if strings.ContainsAny(exe, " \t") {
		return fmt.Sprintf(`"%s" -service %s`, exe, iface)
	}
	return fmt.Sprintf(`%s -service %s`, exe, iface)
}

func updateWindowsServiceBinary(s *mgr.Service, exe, iface string) error {
	cfg, err := s.Config()
	if err != nil {
		return fmt.Errorf("query service config: %w", err)
	}
	want := windowsServiceBinaryPath(exe, iface)
	cfg.BinaryPathName = want
	cfg.StartType = mgr.StartAutomatic
	if cfg.DisplayName == "" {
		cfg.DisplayName = fmt.Sprintf("Lasitan-Cluster (%s)", iface)
	}
	if cfg.Description == "" {
		cfg.Description = "Lasitan-Cluster agent tunnel (WireGuard over TCP + MC camouflage)"
	}
	if err := s.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("update service binary path: %w", err)
	}
	return nil
}

func startWindowsService(m *mgr.Mgr, name string) error {
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()
	return startWindowsServiceHandle(s, name)
}

func startWindowsServiceHandle(s *mgr.Service, name string) error {
	status, err := s.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Running {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: %s already running\n", name)
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: started %s\n", name)
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

	name := WindowsServiceName(iface)
	s, err := m.OpenService(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: service %s not found\n", name)
	} else {
		_, _ = s.Control(svc.Stop)
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
		fmt.Fprintf(os.Stderr, "lasitan-cluster: removed service %s\n", name)
	}
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
		agent := filepath.Join(config.ConfDir(), config.AgentFileName)
		if err := os.Remove(agent); err == nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: removed %s\n", agent)
		}
	} else {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: kept configs under %s (pass --purge to delete)\n", config.ConfDir())
	}
	return nil
}

func ensureWGConfigsWindows(iface string) error {
	dir := config.ConfDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	agentPath := filepath.Join(dir, config.AgentFileName)
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		example := fmt.Sprintf(`{
  "masterUrl": "http://127.0.0.1:8443",
  "key": "CHANGE_ME"
}
`)
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
