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

	"golang.org/x/sys/windows"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/ipc"
	"golang.zx2c4.com/wireguard/tun"
)

const (
	ExitSetupSuccess = 0
	ExitSetupFailed  = 1
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("wireguard-go v%s\n", Version)
		return
	}
	if runAsWindowsServiceIfRequested() {
		return
	}
	if handleKeyCommand() {
		return
	}
	if handleServiceCommand() {
		return
	}
	if len(os.Args) != 2 {
		printUsage()
		os.Exit(ExitSetupFailed)
	}
	interfaceName := os.Args[1]

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

	var natgw *natGateway
	confDir := wgConfDir()
	confFile := filepath.Join(confDir, interfaceName+".conf")
	if _, statErr := os.Stat(confFile); statErr == nil {
		fmt.Fprintf(os.Stderr, "wireguard-go: loading %s\n", confFile)
		result, err := applyWGConf(dev, logger, interfaceName)
		if err != nil {
			logger.Errorf("Failed to apply wg conf: %v", err)
			fmt.Fprintf(os.Stderr, "wireguard-go: failed to apply %s: %v\n", confFile, err)
			os.Exit(ExitSetupFailed)
		}
		if result != nil {
			if result.nat.toNATClient {
				fmt.Fprintln(os.Stderr, "wireguard-go: ToNAT client mode (dial [Peer] Endpoint)")
				tcpBind.SetDialToNAT(true)
			}
			if result.nat.serverMode {
				natgw = newNatGateway(logger, interfaceName, result.nat)
				if err := natgw.Start(dev); err != nil {
					logger.Errorf("Failed to start NAT gateway: %v", err)
					fmt.Fprintf(os.Stderr, "wireguard-go: NAT gateway error: %v\n", err)
					os.Exit(ExitSetupFailed)
				}
				dev.SetNatClientHandler(func(pk device.NoisePublicKey) {
					natgw.RegisterClient(pk)
				})
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: no config at %s (UAPI-only mode; set WG_CONF_DIR to override)\n", confFile)
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

	if os.Getenv("LOG_LEVEL") == "verbose" || os.Getenv("LOG_LEVEL") == "debug" {
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
		printStartupInfo(dev, logger, interfaceName, confFile, tcpPort, true, 0)
	}

	signal.Notify(term, os.Interrupt)
	signal.Notify(term, os.Kill)
	signal.Notify(term, windows.SIGTERM)

	select {
	case <-term:
	case <-errs:
	case <-dev.Wait():
	}

	_ = tcpBind.Close()
	if natgw != nil {
		natgw.Close(dev)
	}
	_ = uapi.Close()
	dev.Close()

	logger.Verbosef("Shutting down")
}
