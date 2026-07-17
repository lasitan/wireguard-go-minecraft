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
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/ipc"
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
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("wireguard-go v%s\n\nUserspace WireGuard daemon for %s-%s.\nInformation available at https://www.wireguard.com.\nCopyright (C) Jason A. Donenfeld <Jason@zx2c4.com>.\n", Version, runtime.GOOS, runtime.GOARCH)
		return
	}

	if handleKeyCommand() {
		return
	}

	var foreground bool
	var interfaceName string
	if len(os.Args) < 2 || len(os.Args) > 3 {
		printUsage()
		return
	}

	switch os.Args[1] {

	case "-f", "--foreground":
		foreground = true
		if len(os.Args) != 3 {
			printUsage()
			return
		}
		interfaceName = os.Args[2]

	default:
		foreground = false
		if len(os.Args) != 2 {
			printUsage()
			return
		}
		interfaceName = os.Args[1]
	}

	if !foreground {
		foreground = os.Getenv(ENV_WG_PROCESS_FOREGROUND) == "1"
	}

	// get log level (default: info)

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

	// open TUN device (or use supplied fd)

	tdev, err := func() (tun.Device, error) {
		tunFdStr := os.Getenv(ENV_WG_TUN_FD)
		if tunFdStr == "" {
			return tun.CreateTUN(interfaceName, device.DefaultMTU)
		}

		// construct tun device from supplied fd

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

	// open UAPI file (or use supplied fd)

	fileUAPI, err := func() (*os.File, error) {
		uapiFdStr := os.Getenv(ENV_WG_UAPI_FD)
		if uapiFdStr == "" {
			return ipc.UAPIOpen(interfaceName)
		}

		// use supplied fd

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
	// daemonize the process

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
				files[0], // stdin
				files[1], // stdout
				files[2], // stderr
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

		process, err := os.StartProcess(
			path,
			os.Args,
			attr,
		)
		if err != nil {
			logger.Errorf("Failed to daemonize: %v", err)
			os.Exit(ExitSetupFailed)
		}
		process.Release()
		return
	}

	tcpBind := conn.NewTCPBind()
	dev := device.NewDevice(tdev, tcpBind, logger)
	logger.Verbosef("Transport mode: TCP + MC-Camouflage")

	if err := dev.Up(); err != nil {
		logger.Errorf("Failed to bring up device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	fwd := newPortForwardManager(logger)

	// Auto-load /etc/wireguard/<iface>.conf if it exists.
	confPath := ""
	confFile := "/etc/wireguard/" + interfaceName + ".conf"
	fwdCount := 0
	if _, statErr := os.Stat(confFile); statErr == nil {
		confPath = confFile
		fmt.Fprintf(os.Stderr, "wireguard-go: loading %s\n", confFile)
		result, err := applyWGConf(dev, logger, interfaceName)
		if err != nil {
			logger.Errorf("Failed to apply wg conf: %v", err)
			fmt.Fprintf(os.Stderr, "wireguard-go: failed to apply %s: %v\n", confFile, err)
			os.Exit(ExitSetupFailed)
		}
		if result != nil {
			if len(result.netCfg.addresses) == 0 {
				fmt.Fprintf(os.Stderr, "wireguard-go: WARNING: no Address= in %s — interface will have no IP\n", confFile)
			}
			fmt.Fprintln(os.Stderr, "wireguard-go: starting port forwards (if any)")
			if err := fwd.StartFromPeers(result.peers); err != nil {
				logger.Errorf("Failed to start port forwards: %v", err)
				fmt.Fprintf(os.Stderr, "wireguard-go: port forward error: %v\n", err)
				os.Exit(ExitSetupFailed)
			}
			fwdCount = fwd.Count()
		}
	} else {
		fmt.Fprintf(os.Stderr, "wireguard-go: no config at %s (UAPI-only mode)\n", confFile)
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
			conn, err := uapi.Accept()
			if err != nil {
				errs <- err
				return
			}
			go dev.IpcHandle(conn)
		}
	}()

	logger.Verbosef("UAPI listener started")

	// Print startup summary (port, peers, camouflage status) when in foreground.
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
		printStartupInfo(dev, logger, interfaceName, confPath, tcpPort, mcEnabled, fwdCount)
		fmt.Fprintln(os.Stderr, "wireguard-go: ready (foreground). Waiting for peers — Ctrl+C to stop.")
		if os.Getenv("LOG_LEVEL") == "" {
			fmt.Fprintln(os.Stderr, "wireguard-go: tip: LOG_LEVEL=verbose for handshake/TCP logs")
		}
	}

	// wait for program to terminate

	signal.Notify(term, unix.SIGTERM)
	signal.Notify(term, os.Interrupt)

	select {
	case <-term:
	case <-errs:
	case <-dev.Wait():
	}

	// Interrupt TCP dials / sessions first so peer.Stop cannot block on Send.
	_ = tcpBind.Close()
	fwd.Close()
	_ = uapi.Close()
	dev.Close()

	logger.Verbosef("Shutting down")
	// Force exit: leftover forward/accept goroutines must not keep the process alive.
	os.Exit(0)
}
