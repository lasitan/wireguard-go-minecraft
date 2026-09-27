//go:build windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"golang.zx2c4.com/wireguard/meshcfg"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/internal/agent"
	"golang.zx2c4.com/wireguard/internal/service"
	"golang.zx2c4.com/wireguard/internal/tunnel"
	"golang.zx2c4.com/wireguard/ipc"
	"golang.zx2c4.com/wireguard/tun"
)

// runAsWindowsServiceIfRequested handles SCM startup:
//
//	wireguard-go -service INTERFACE
//
// Returns true if this process was (or claimed to be) a Windows service entry.
func runAsWindowsServiceIfRequested() bool {
	if len(os.Args) >= 3 && os.Args[1] == "-service" {
		iface := os.Args[2]
		if err := svc.Run(service.WindowsServiceName(iface), &wgWindowsService{iface: iface}); err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: service error: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		return true
	}
	// When launched by SCM without our -service args, IsWindowsService is true
	// but we cannot know the iface — require -service from CreateService.
	return false
}

type wgWindowsService struct {
	iface string
}

func (m *wgWindowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	logger := device.NewLogger(device.LogLevelError, fmt.Sprintf("(%s) ", m.iface))
	if os.Getenv("LOG_LEVEL") == "verbose" || os.Getenv("LOG_LEVEL") == "debug" {
		logger = device.NewLogger(device.LogLevelVerbose, fmt.Sprintf("(%s) ", m.iface))
	}

	if err := ensureWintunDLL(); err != nil {
		logger.Errorf("wintun.dll: %v", err)
		return true, 1
	}
	tdev, err := tun.CreateTUN(m.iface, 0)
	if err != nil {
		logger.Errorf("TUN: %v", err)
		return true, 1
	}
	if name, err := tdev.Name(); err == nil {
		m.iface = name
	}

	tcpBind := conn.NewTCPBind().(*conn.TCPBind)
	dev := device.NewDevice(tdev, tcpBind, logger)
	if err := dev.Up(); err != nil {
		logger.Errorf("Up: %v", err)
		_ = tcpBind.Close()
		dev.Close()
		return true, 1
	}

	var natgw *tunnel.NatGateway
	fwd := tunnel.NewPortForwardManager(logger)
	var fwdMu sync.Mutex
	agentStop := make(chan struct{})
	if boot, err := agent.LoadBootstrap(); err == nil {
		go agent.ConfigLoop(dev, logger, m.iface, &fwd, &fwdMu, boot, agentStop)
	} else if os.Getenv("WG_LEGACY_CONF") == "1" {
		confFile := filepath.Join(meshcfg.ConfDir(), m.iface+".conf")
		if _, err := os.Stat(confFile); err == nil {
			result, err := tunnel.ApplyWGConf(dev, logger, m.iface)
			if err != nil {
				logger.Errorf("conf: %v", err)
				_ = tcpBind.Close()
				dev.Close()
				return true, 1
			}
			if result != nil && result.Nat.ToNATClient {
				tcpBind.SetDialToNAT(true)
			}
			if result != nil && result.Nat.ServerMode {
				natgw = tunnel.NewNatGateway(logger, m.iface, result.Nat)
				if err := natgw.Start(dev); err != nil {
					logger.Errorf("NAT gateway: %v", err)
					_ = tcpBind.Close()
					dev.Close()
					return true, 1
				}
				dev.SetNatClientHandler(func(pk device.NoisePublicKey) {
					natgw.RegisterClient(pk)
				})
			}
			_ = fwd.StartFromPeers(result.Peers)
		}
	}

	uapi, err := ipc.UAPIListen(m.iface)
	if err != nil {
		logger.Errorf("UAPI: %v", err)
		close(agentStop)
		if natgw != nil {
			natgw.Close(dev)
		}
		_ = tcpBind.Close()
		dev.Close()
		return true, 1
	}

	go func() {
		for {
			c, err := uapi.Accept()
			if err != nil {
				return
			}
			go dev.IpcHandle(c)
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			default:
				continue loop
			}
		case <-dev.Wait():
			break loop
		}
	}

	changes <- svc.Status{State: svc.StopPending}
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
	time.Sleep(200 * time.Millisecond)
	changes <- svc.Status{State: svc.Stopped}
	return false, 0
}
