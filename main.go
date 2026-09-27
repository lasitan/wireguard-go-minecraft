//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
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

const (
	ENV_WG_TUN_FD             = "WG_TUN_FD"
	ENV_WG_UAPI_FD            = "WG_UAPI_FD"
	ENV_WG_PROCESS_FOREGROUND = "WG_PROCESS_FOREGROUND"
)

func main() {
	update.SetCurrent(Version)
	update.CleanupOld()
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("wireguard-go v%s\n\nUserspace WireGuard daemon for %s-%s.\nInformation available at https://www.wireguard.com.\nCopyright (C) Jason A. Donenfeld <Jason@zx2c4.com>.\n", update.Current(), runtime.GOOS, runtime.GOARCH)
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
	if len(os.Args) < 2 || len(os.Args) > 3 {
		tunnel.PrintUsage()
		return
	}

	switch os.Args[1] {

	case "-f", "--foreground":
		foreground = true
		if len(os.Args) != 3 {
			tunnel.PrintUsage()
			return
		}
		interfaceName = os.Args[2]

	default:
		foreground = false
		if len(os.Args) != 2 {
			tunnel.PrintUsage()
			return
		}
		interfaceName = os.Args[1]
	}

	if !foreground {
		foreground = os.Getenv(ENV_WG_PROCESS_FOREGROUND) == "1"
	}

	logLevel := func() int {
		switch os.Getenv("LOG_LEVEL") {
		case "verbose", "debug":
			return device.LogLevelVerbose
		case "error":
			return device.LogLevelError
		case "silent":
			return device.LogLevelSilent
		}
		return device.LogLevelError
	}()

	tdev, err := func() (tun.Device, error) {
		tunFdStr := os.Getenv(ENV_WG_TUN_FD)
		if tunFdStr == "" {
			return tun.CreateTUN(interfaceName, device.DefaultMTU)
		}
		fd, err := strconv.ParseUint(tunFdStr, 10, 32)
		if err != nil {
			return nil, err
		}
		err = unix.SetNonblock(int(fd), true)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), "")
		return tun.CreateTUNFromFile(file, device.DefaultMTU)
	}()

	if err == nil {
		realInterfaceName, err2 := tdev.Name()
		if err2 == nil {
			interfaceName = realInterfaceName
			fmt.Fprintf(os.Stderr, "wireguard-go: TUN interface %q created\n", interfaceName)
		}
	}

	logger := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName),
	)

	logger.Verbosef("Starting wireguard-go version %s", Version)

	if err != nil {
		logger.Errorf("Failed to create TUN device: %v", err)
		fmt.Fprintln(os.Stderr, "wireguard-go: TUN creation failed — run as root (or CAP_NET_ADMIN) and ensure /dev/net/tun exists")
		os.Exit(ExitSetupFailed)
	}

	fileUAPI, err := func() (*os.File, error) {
		uapiFdStr := os.Getenv(ENV_WG_UAPI_FD)
		if uapiFdStr == "" {
			return ipc.UAPIOpen(interfaceName)
		}
		fd, err := strconv.ParseUint(uapiFdStr, 10, 32)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), ""), nil
	}()
	if err != nil {
		logger.Errorf("UAPI listen error: %v", err)
		os.Exit(ExitSetupFailed)
		return
	}

	if !foreground {
		env := os.Environ()
		env = append(env, fmt.Sprintf("%s=3", ENV_WG_TUN_FD))
		env = append(env, fmt.Sprintf("%s=4", ENV_WG_UAPI_FD))
		env = append(env, fmt.Sprintf("%s=1", ENV_WG_PROCESS_FOREGROUND))
		files := [3]*os.File{}
		if os.Getenv("LOG_LEVEL") != "" && logLevel != device.LogLevelSilent {
			files[0], _ = os.Open(os.DevNull)
			files[1] = os.Stdout
			files[2] = os.Stderr
		} else {
			files[0], _ = os.Open(os.DevNull)
			files[1], _ = os.Open(os.DevNull)
			files[2] = os.Stderr
		}
		attr := &os.ProcAttr{
			Files: []*os.File{
				files[0],
				files[1],
				files[2],
				tdev.File(),
				fileUAPI,
			},
			Dir: ".",
			Env: env,
		}

		path, err := os.Executable()
		if err != nil {
			logger.Errorf("Failed to determine executable: %v", err)
			os.Exit(ExitSetupFailed)
		}

		process, err := os.StartProcess(path, os.Args, attr)
		if err != nil {
			logger.Errorf("Failed to daemonize: %v", err)
			os.Exit(ExitSetupFailed)
		}
		process.Release()
		return
	}

	tcpBind := conn.NewTCPBind().(*conn.TCPBind)
	dev := device.NewDevice(tdev, tcpBind, logger)
	logger.Verbosef("Transport mode: TCP + MC-Camouflage")

	if err := dev.Up(); err != nil {
		logger.Errorf("Failed to bring up device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	fwd := tunnel.NewPortForwardManager(logger)
	var fwdMu sync.Mutex
	var natgw *tunnel.NatGateway
	agentStop := make(chan struct{})

	fwdCount := 0
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
				if len(result.NetCfg.Addresses()) == 0 {
					fmt.Fprintf(os.Stderr, "wireguard-go: WARNING: no Address= in %s — interface will have no IP\n", confFile)
				}
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
				fmt.Fprintln(os.Stderr, "wireguard-go: starting port forwards (if any)")
				if err := fwd.StartFromPeers(result.Peers); err != nil {
					logger.Errorf("Failed to start port forwards: %v", err)
					fmt.Fprintf(os.Stderr, "wireguard-go: port forward error: %v\n", err)
					os.Exit(ExitSetupFailed)
				}
				fwdCount = fwd.Count()
			}
		} else {
			fmt.Fprintf(os.Stderr, "wireguard-go: no agent bootstrap and no legacy conf at %s\n", confFile)
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: waiting for %s (or set WG_LEGACY_CONF=1 for wg0.conf)\n", agent.BootstrapPath())
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	uapi, err := ipc.UAPIListen(interfaceName, fileUAPI)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		os.Exit(ExitSetupFailed)
	}

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

	if foreground {
		var ipcBuf bytes.Buffer
		tcpPort := uint16(0)
		mcEnabled := true
		if err := dev.IpcGetOperation(&ipcBuf); err == nil {
			for _, l := range strings.Split(ipcBuf.String(), "\n") {
				if strings.HasPrefix(l, "listen_port=") {
					v := strings.TrimPrefix(l, "listen_port=")
					if p, err := strconv.ParseUint(v, 10, 16); err == nil {
						tcpPort = uint16(p)
					}
				}
			}
		}
		tunnel.PrintStartupInfo(dev, logger, interfaceName, confPath, tcpPort, mcEnabled, fwdCount)
		fmt.Fprintln(os.Stderr, "wireguard-go: ready (foreground). Waiting for peers — Ctrl+C to stop.")
		if os.Getenv("LOG_LEVEL") == "" {
			fmt.Fprintln(os.Stderr, "wireguard-go: tip: LOG_LEVEL=verbose for handshake/TCP logs")
		}
	}

	signal.Notify(term, unix.SIGTERM)
	signal.Notify(term, os.Interrupt)

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
	os.Exit(0)
}
