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
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
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
		if err := svc.Run(windowsServiceName(iface), &wgWindowsService{iface: iface}); err != nil {
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

	var natgw *natGateway
	confFile := filepath.Join(wgConfDir(), m.iface+".conf")
	if _, err := os.Stat(confFile); err == nil {
		result, err := applyWGConf(dev, logger, m.iface)
		if err != nil {
			logger.Errorf("conf: %v", err)
			_ = tcpBind.Close()
			dev.Close()
			return true, 1
		}
		if result != nil && result.nat.toNATClient {
			tcpBind.SetDialToNAT(true)
		}
		if result != nil && result.nat.serverMode {
			natgw = newNatGateway(logger, m.iface, result.nat)
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
	}

	uapi, err := ipc.UAPIListen(m.iface)
	if err != nil {
		logger.Errorf("UAPI: %v", err)
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
	_ = tcpBind.Close()
	if natgw != nil {
		natgw.Close(dev)
	}
	_ = uapi.Close()
	dev.Close()
	// Give goroutines a moment before SCM tears us down.
	time.Sleep(200 * time.Millisecond)
	changes <- svc.Status{State: svc.Stopped}
	return false, 0
}
