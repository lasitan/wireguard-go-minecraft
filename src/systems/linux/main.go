//go:build linux

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package linux

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

	"golang.zx2c4.com/wireguard/src/core"

	"golang.org/x/sys/unix"

	"golang.zx2c4.com/wireguard/src/agent"
	"golang.zx2c4.com/wireguard/src/commands/keygen"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/core/version"
	tunconf "golang.zx2c4.com/wireguard/src/tunnel/config"
	"golang.zx2c4.com/wireguard/src/tunnel/natgw"
	"golang.zx2c4.com/wireguard/src/tunnel/portfwd"
	"golang.zx2c4.com/wireguard/src/update"
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
	"golang.zx2c4.com/wireguard/src/wireguard/ipc"
	"golang.zx2c4.com/wireguard/src/wireguard/tun"
)

const (
	ENV_WG_TUN_FD             = "WG_TUN_FD"
	ENV_WG_UAPI_FD            = "WG_UAPI_FD"
	ENV_WG_PROCESS_FOREGROUND = "WG_PROCESS_FOREGROUND"
)

// VersionText is printed by `lasitan-cluster --version`.
func VersionText() string {
	return fmt.Sprintf("lasitan-cluster v%s\n\nLasitan-Cluster userspace mesh daemon for %s-%s.\n", update.Current(), runtime.GOOS, runtime.GOARCH)
}

// Main runs service subcommands or the tunnel daemon for os.Args.
func Main() {
	if HandleCommand() {
		return
	}

	var foreground bool
	var interfaceName string
	if len(os.Args) < 2 || len(os.Args) > 3 {
		keygen.PrintUsage()
		return
	}

	switch os.Args[1] {

	case "-f", "--foreground":
		foreground = true
		if len(os.Args) != 3 {
			keygen.PrintUsage()
			return
		}
		interfaceName = os.Args[2]

	default:
		foreground = false
		if len(os.Args) != 2 {
			keygen.PrintUsage()
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
			fmt.Fprintf(os.Stderr, "lasitan-cluster: TUN interface %q created\n", interfaceName)
		}
	}

	logger := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName),
	)

	logger.Verbosef("Starting lasitan-cluster version %s", version.Version)

	if err != nil {
		logger.Errorf("Failed to create TUN device: %v", err)
		fmt.Fprintln(os.Stderr, "lasitan-cluster: TUN creation failed — run as root (or CAP_NET_ADMIN) and ensure /dev/net/tun exists")
		os.Exit(core.ExitSetupFailed)
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
		os.Exit(core.ExitSetupFailed)
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
			os.Exit(core.ExitSetupFailed)
		}

		process, err := os.StartProcess(path, os.Args, attr)
		if err != nil {
			logger.Errorf("Failed to daemonize: %v", err)
			os.Exit(core.ExitSetupFailed)
		}
		process.Release()
		return
	}

	tcpBind := conn.NewTCPBind().(*conn.TCPBind)
	dev := device.NewDevice(tdev, tcpBind, logger)
	logger.Verbosef("Transport mode: TCP + MC-Camouflage")

	if err := dev.Up(); err != nil {
		logger.Errorf("Failed to bring up device: %v", err)
		os.Exit(core.ExitSetupFailed)
	}

	fwd := portfwd.NewPortForwardManager(logger)
	var fwdMu sync.Mutex
	var gateway *natgw.NatGateway
	agentStop := make(chan struct{})

	fwdCount := 0
	confPath := ""
	if boot, err := agent.LoadBootstrap(); err == nil {
		unlock, err := agent.LockAgentInstance()
		if err != nil {
			logger.Errorf("%v", err)
			fmt.Fprintf(os.Stderr, "lasitan-cluster: %v\n", err)
			os.Exit(core.ExitSetupFailed)
		}
		defer unlock()
		fmt.Fprintf(os.Stderr, "lasitan-cluster: agent mode → %s (nodeId from Master)\n", boot.MasterURL)
		go agent.ConfigLoop(dev, logger, interfaceName, &fwd, &fwdMu, boot, agentStop)
	} else if os.Getenv("LASITAN_LEGACY_CONF") == "1" {
		confFile := filepath.Join(config.ConfDir(), interfaceName+".conf")
		if _, statErr := os.Stat(confFile); statErr == nil {
			confPath = confFile
			fmt.Fprintf(os.Stderr, "lasitan-cluster: loading legacy %s\n", confFile)
			result, err := tunconf.ApplyWGConf(dev, logger, interfaceName)
			if err != nil {
				logger.Errorf("Failed to apply wg conf: %v", err)
				fmt.Fprintf(os.Stderr, "lasitan-cluster: failed to apply %s: %v\n", confFile, err)
				os.Exit(core.ExitSetupFailed)
			}
			if result != nil {
				if len(result.NetCfg.Addresses()) == 0 {
					fmt.Fprintf(os.Stderr, "lasitan-cluster: WARNING: no Address= in %s — interface will have no IP\n", confFile)
				}
				if result.Nat.ToNATClient {
					fmt.Fprintln(os.Stderr, "lasitan-cluster: ToNAT client mode (dial [Peer] Endpoint)")
					tcpBind.SetDialToNAT(true)
				}
				if result.Nat.ServerMode {
					gateway = natgw.NewNatGateway(logger, interfaceName, result.Nat)
					if err := gateway.Start(dev); err != nil {
						logger.Errorf("Failed to start NAT gateway: %v", err)
						fmt.Fprintf(os.Stderr, "lasitan-cluster: NAT gateway error: %v\n", err)
						os.Exit(core.ExitSetupFailed)
					}
					dev.SetNatClientHandler(func(pk device.NoisePublicKey) {
						gateway.RegisterClient(pk)
					})
				}
				fmt.Fprintln(os.Stderr, "lasitan-cluster: starting port forwards (if any)")
				if err := fwd.StartFromPeers(result.Peers); err != nil {
					logger.Errorf("Failed to start port forwards: %v", err)
					fmt.Fprintf(os.Stderr, "lasitan-cluster: port forward error: %v\n", err)
					os.Exit(core.ExitSetupFailed)
				}
				fwdCount = fwd.Count()
			}
		} else {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: no agent bootstrap and no legacy conf at %s\n", confFile)
		}
	} else {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: waiting for %s (or set LASITAN_LEGACY_CONF=1 for lc0.conf)\n", agent.BootstrapPath())
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	uapi, err := ipc.UAPIListen(interfaceName, fileUAPI)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		os.Exit(core.ExitSetupFailed)
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
		tunconf.PrintStartupInfo(dev, logger, interfaceName, confPath, tcpPort, mcEnabled, fwdCount)
		fmt.Fprintln(os.Stderr, "lasitan-cluster: ready (foreground). Waiting for peers — Ctrl+C to stop.")
		if os.Getenv("LOG_LEVEL") == "" {
			fmt.Fprintln(os.Stderr, "lasitan-cluster: tip: LOG_LEVEL=verbose for handshake/TCP logs")
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
	if gateway != nil {
		gateway.Close(dev)
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
