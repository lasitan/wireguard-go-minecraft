/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/internal/agent"
	"golang.zx2c4.com/wireguard/internal/service"
	"golang.zx2c4.com/wireguard/internal/tunnel"
	"golang.zx2c4.com/wireguard/internal/update"
	"golang.zx2c4.com/wireguard/ipc"
	"golang.zx2c4.com/wireguard/meshcfg"
	"golang.zx2c4.com/wireguard/tun"
)

const (
	ExitSetupSuccess = 0
	ExitSetupFailed  = 1
)

func main() {
	update.SetCurrent(Version)
	update.CleanupOld()
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("wireguard-go v%s\n", update.Current())
		return
	}
	if runAsWindowsServiceIfRequested() {
		return
	}
	if tunnel.HandleKeyCommand() {
		return
	}
	if handleMasterCommand() {
		return
	}
	if service.HandleCommand() {
		return
	}

	var foreground bool
	var interfaceName string
	switch {
	case len(os.Args) == 3 && (os.Args[1] == "-f" || os.Args[1] == "--foreground"):
		foreground = true
		interfaceName = os.Args[2]
	case len(os.Args) == 2:
		interfaceName = os.Args[1]
		if interfaceName == "-f" || interfaceName == "--foreground" {
			tunnel.PrintUsage()
			os.Exit(ExitSetupFailed)
		}
	default:
		tunnel.PrintUsage()
		os.Exit(ExitSetupFailed)
	}
	if strings.ContainsAny(interfaceName, `/\`) || strings.HasSuffix(strings.ToLower(interfaceName), ".conf") {
		fmt.Fprintf(os.Stderr, "wireguard-go: pass interface name (e.g. wg0), not a config path\n")
		fmt.Fprintf(os.Stderr, "wireguard-go: config is loaded from %s\\<iface>.conf\n", meshcfg.ConfDir())
		os.Exit(ExitSetupFailed)
	}

	if err := ensureWintunDLL(); err != nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: wintun.dll: %v\n", err)
		os.Exit(ExitSetupFailed)
	}
	if err := service.EnsureTransportConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: transport config: %v\n", err)
		os.Exit(ExitSetupFailed)
	}

	logLevel := device.LogLevelError
	switch os.Getenv("LOG_LEVEL") {
	case "verbose", "debug":
		logLevel = device.LogLevelVerbose
	case "silent":
		logLevel = device.LogLevelSilent
	}

	logger := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName),
	)
	logger.Verbosef("Starting wireguard-go version %s", Version)

	tdev, err := tun.CreateTUN(interfaceName, 0)
	if err == nil {
		realInterfaceName, err2 := tdev.Name()
		if err2 == nil {
			interfaceName = realInterfaceName
		}
	} else {
		logger.Errorf("Failed to create TUN device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	tcpBind := conn.NewTCPBind().(*conn.TCPBind)
	dev := device.NewDevice(tdev, tcpBind, logger)
	logger.Verbosef("Transport mode: TCP + MC-Camouflage")
	if err := dev.Up(); err != nil {
		logger.Errorf("Failed to bring up device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	var natgw *tunnel.NatGateway
	fwd := tunnel.NewPortForwardManager(logger)
	var fwdMu sync.Mutex
	agentStop := make(chan struct{})
	confPath := ""
	if boot, err := agent.LoadBootstrap(); err == nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: agent mode → %s (nodeId from Master)\n", boot.MasterURL)
		go agent.ConfigLoop(dev, logger, interfaceName, &fwd, &fwdMu, boot, agentStop)
	} else if os.Getenv("WG_LEGACY_CONF") == "1" {
		confFile := filepath.Join(meshcfg.ConfDir(), interfaceName+".conf")
		if _, statErr := os.Stat(confFile); statErr == nil {
			confPath = confFile
			fmt.Fprintf(os.Stderr, "wireguard-go: loading legacy %s\n", confFile)
			result, err := tunnel.ApplyWGConf(dev, logger, interfaceName)
			if err != nil {
				logger.Errorf("Failed to apply wg conf: %v", err)
				fmt.Fprintf(os.Stderr, "wireguard-go: failed to apply %s: %v\n", confFile, err)
				os.Exit(ExitSetupFailed)
			}
			if result != nil {
				if result.Nat.ToNATClient {
					fmt.Fprintln(os.Stderr, "wireguard-go: ToNAT client mode (dial [Peer] Endpoint)")
					tcpBind.SetDialToNAT(true)
				}
				if result.Nat.ServerMode {
					natgw = tunnel.NewNatGateway(logger, interfaceName, result.Nat)
					if err := natgw.Start(dev); err != nil {
						logger.Errorf("Failed to start NAT gateway: %v", err)
						fmt.Fprintf(os.Stderr, "wireguard-go: NAT gateway error: %v\n", err)
						os.Exit(ExitSetupFailed)
					}
					dev.SetNatClientHandler(func(pk device.NoisePublicKey) {
						natgw.RegisterClient(pk)
					})
				}
				_ = fwd.StartFromPeers(result.Peers)
			}
		} else {
			fmt.Fprintf(os.Stderr, "wireguard-go: no agent bootstrap and no legacy conf at %s\n", confFile)
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: waiting for %s (or set WG_LEGACY_CONF=1 for .conf)\n", agent.BootstrapPath())
	}

	logger.Verbosef("Device started")

	uapi, err := ipc.UAPIListen(interfaceName)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		os.Exit(ExitSetupFailed)
	}

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	go func() {
		for {
			c, err := uapi.Accept()
			if err != nil {
				errs <- err
				return
			}
			go dev.IpcHandle(c)
		}
	}()
	logger.Verbosef("UAPI listener started")

	if foreground || os.Getenv("LOG_LEVEL") == "verbose" || os.Getenv("LOG_LEVEL") == "debug" {
		var tcpPort uint16
		if ipcStr, err := dev.IpcGet(); err == nil {
			for _, l := range strings.Split(ipcStr, "\n") {
				if strings.HasPrefix(l, "listen_port=") {
					v := strings.TrimPrefix(l, "listen_port=")
					if p, err := strconv.ParseUint(v, 10, 16); err == nil {
						tcpPort = uint16(p)
					}
				}
			}
		}
		tunnel.PrintStartupInfo(dev, logger, interfaceName, confPath, tcpPort, true, fwd.Count())
		fmt.Fprintln(os.Stderr, "wireguard-go: ready (foreground). Waiting for peers — Ctrl+C to stop.")
	}

	signal.Notify(term, os.Interrupt)
	signal.Notify(term, os.Kill)
	signal.Notify(term, windows.SIGTERM)

	select {
	case <-term:
	case <-errs:
	case <-dev.Wait():
	}

	close(agentStop)
	_ = tcpBind.Close()
	if natgw != nil {
		natgw.Close(dev)
	}
	fwdMu.Lock()
	if fwd != nil {
		fwd.Close()
	}
	fwdMu.Unlock()
	_ = uapi.Close()
	dev.Close()

	logger.Verbosef("Shutting down")
}
