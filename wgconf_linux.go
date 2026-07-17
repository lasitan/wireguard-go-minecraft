//go:build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

const wgConfDir = "/etc/wireguard"

type ifaceNetConfig struct {
	addresses []string
	mtu       int
}

// peerHookConfig holds per-peer extras that are not part of WireGuard UAPI.
type peerHookConfig struct {
	label     string // truncated public key for logs
	allowedIP string // first usable host from AllowedIPs (for ForwardTCP/UDP shorthand)
	forwards  []portForwardSpec
	onUp      []string
	onDown    []string
}

type confApplyResult struct {
	netCfg ifaceNetConfig
	peers  []peerHookConfig
}

// applyWGConf reads /etc/wireguard/<iface>.conf, applies UAPI settings,
// configures Address/MTU, starts ForwardTCP proxies, and runs peer OnUp scripts.
func applyWGConf(dev *device.Device, logger *device.Logger, iface string) (*confApplyResult, error) {
	confPath := filepath.Join(wgConfDir, iface+".conf")
	f, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &confApplyResult{}, nil
		}
		return nil, fmt.Errorf("open %s: %w", confPath, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := &buf
	expectedPeers := 0
	var netCfg ifaceNetConfig
	var peers []peerHookConfig
	var cur *peerHookConfig

	flushPeer := func() {
		if cur != nil {
			peers = append(peers, *cur)
			cur = nil
		}
	}

	var inInterface, inPeer bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			switch strings.ToLower(line) {
			case "[interface]":
				flushPeer()
				inInterface, inPeer = true, false
			case "[peer]":
				flushPeer()
				inInterface, inPeer = false, true
				cur = &peerHookConfig{}
			default:
				flushPeer()
				inInterface, inPeer = false, false
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])

		if inInterface {
			switch strings.ToLower(key) {
			case "privatekey":
				hexKey, err := base64ToHex(val)
				if err != nil {
					return nil, fmt.Errorf("PrivateKey: %w", err)
				}
				fmt.Fprintf(w, "private_key=%s\n", hexKey)
			case "listenport":
				fmt.Fprintf(w, "listen_port=%s\n", val)
			case "fwmark":
				fmt.Fprintf(w, "fwmark=%s\n", val)
			case "address":
				for _, a := range strings.Split(val, ",") {
					a = strings.TrimSpace(a)
					if a != "" {
						netCfg.addresses = append(netCfg.addresses, a)
					}
				}
			case "mtu":
				mtu, err := strconv.Atoi(val)
				if err != nil || mtu <= 0 {
					return nil, fmt.Errorf("invalid MTU %q", val)
				}
				netCfg.mtu = mtu
			}
			continue
		}

		if inPeer && cur != nil {
			switch strings.ToLower(key) {
			case "publickey":
				hexKey, err := base64ToHex(val)
				if err != nil {
					return nil, fmt.Errorf("PublicKey: %w", err)
				}
				fmt.Fprintf(w, "public_key=%s\n", hexKey)
				expectedPeers++
				if len(val) > 8 {
					cur.label = val[:4] + "…" + val[len(val)-4:]
				} else {
					cur.label = val
				}
			case "presharedkey":
				hexKey, err := base64ToHex(val)
				if err != nil {
					return nil, fmt.Errorf("PresharedKey: %w", err)
				}
				fmt.Fprintf(w, "preshared_key=%s\n", hexKey)
			case "endpoint":
				fmt.Fprintf(w, "endpoint=%s\n", val)
			case "allowedips":
				for _, cidr := range strings.Split(val, ",") {
					cidr = strings.TrimSpace(cidr)
					if cidr == "" {
						continue
					}
					fmt.Fprintf(w, "allowed_ip=%s\n", cidr)
					if cur.allowedIP == "" {
						if host, err := firstHostFromCIDR(cidr); err == nil {
							cur.allowedIP = host
						}
					}
				}
			case "persistentkeepalive":
				fmt.Fprintf(w, "persistent_keepalive_interval=%s\n", val)
			case "forwardtcp":
				specs, err := parseForwardList(val, cur.allowedIP, protoTCP)
				if err != nil {
					return nil, fmt.Errorf("ForwardTCP: %w", err)
				}
				cur.forwards = append(cur.forwards, specs...)
			case "forwardudp":
				specs, err := parseForwardList(val, cur.allowedIP, protoUDP)
				if err != nil {
					return nil, fmt.Errorf("ForwardUDP: %w", err)
				}
				cur.forwards = append(cur.forwards, specs...)
			case "onup", "postup":
				if val != "" {
					cur.onUp = append(cur.onUp, val)
				}
			case "ondown", "predown":
				if val != "" {
					cur.onDown = append(cur.onDown, val)
				}
			}
			continue
		}
	}
	flushPeer()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", confPath, err)
	}

	// Resolve ForwardTCP/UDP shorthand that depended on AllowedIPs order
	// (AllowedIPs may appear after Forward* lines). Detect listen collisions.
	seenListen := make(map[string]string) // "tcp/0.0.0.0:25565" -> peer label
	for i := range peers {
		p := &peers[i]
		resolved := make([]portForwardSpec, 0, len(p.forwards))
		for _, fw := range p.forwards {
			if fw.DestHost == "" {
				if p.allowedIP == "" {
					return nil, fmt.Errorf("peer %s: Forward%s needs AllowedIPs host or explicit dest IP", p.label, strings.ToUpper(fw.Proto))
				}
				fw.DestHost = p.allowedIP
			}
			key := fw.Proto + "/" + fw.ListenAddr()
			if prev, ok := seenListen[key]; ok {
				return nil, fmt.Errorf("duplicate Forward%s listen %s (peer %s and %s)", strings.ToUpper(fw.Proto), fw.ListenAddr(), prev, p.label)
			}
			seenListen[key] = p.label
			resolved = append(resolved, fw)
		}
		p.forwards = resolved
	}

	if buf.Len() > 0 {
		fmt.Fprintf(os.Stderr, "wireguard-go: applying UAPI (bind TCP ListenPort)…\n")
		if err := dev.IpcSetOperation(bufio.NewReader(&buf)); err != nil {
			return nil, fmt.Errorf("apply conf %s: %w", confPath, err)
		}
	}

	if expectedPeers > 0 {
		applied := countConfiguredPeers(dev)
		if applied == 0 {
			return nil, fmt.Errorf(
				"config has %d [Peer] PublicKey entr(y/ies) but 0 peers were kept — "+
					"[Peer] PublicKey must be the *remote* peer's public key, not your own",
				expectedPeers,
			)
		}
		if applied < expectedPeers {
			logger.Verbosef("Warning: config listed %d peers but only %d were applied", expectedPeers, applied)
		}
	}

	fmt.Fprintf(os.Stderr, "wireguard-go: configuring interface %s (mtu/address)…\n", iface)
	if err := applyIfaceNetConfig(iface, netCfg, logger); err != nil {
		return nil, err
	}

	logger.Verbosef("Applied config from %s", confPath)
	return &confApplyResult{netCfg: netCfg, peers: peers}, nil
}

func applyIfaceNetConfig(iface string, cfg ifaceNetConfig, logger *device.Logger) error {
	const ipTimeout = 5 * time.Second
	runIP := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), ipTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ip %v timed out after %s", args, ipTimeout)
			}
			return fmt.Errorf("ip %v: %w (%s)", args, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	if cfg.mtu > 0 {
		if err := runIP("link", "set", "dev", iface, "mtu", strconv.Itoa(cfg.mtu)); err != nil {
			return fmt.Errorf("set mtu %d on %s: %w", cfg.mtu, iface, err)
		}
		logger.Verbosef("Set %s mtu %d", iface, cfg.mtu)
	}
	if err := runIP("link", "set", "dev", iface, "up"); err != nil {
		return fmt.Errorf("set %s up: %w", iface, err)
	}
	for _, addr := range cfg.addresses {
		if err := runIP("addr", "replace", addr, "dev", iface); err != nil {
			return fmt.Errorf("addr %s on %s: %w", addr, iface, err)
		}
		logger.Verbosef("Assigned %s to %s", addr, iface)
		fmt.Fprintf(os.Stderr, "wireguard-go: assigned %s to %s\n", addr, iface)
	}
	return nil
}

func firstHostFromCIDR(cidr string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		// bare IP without prefix
		if ip, err2 := netip.ParseAddr(cidr); err2 == nil {
			return ip.String(), nil
		}
		return "", err
	}
	addr := prefix.Addr()
	if prefix.IsSingleIP() {
		return addr.String(), nil
	}
	// For /24 etc. use network+1 as conventional host; prefer the prefix addr itself
	// if it's not the network address representation... ParsePrefix.Addr() is the
	// masked network. For AllowedIPs like 10.0.0.3/32 we already hit IsSingleIP.
	// For 10.0.0.0/24 there is no unique peer host — caller must specify dest.
	return "", fmt.Errorf("%s is not a single-host prefix (use /32 or /128, or explicit ForwardTCP dest)", cidr)
}

func parseForwardList(val, defaultHost, proto string) ([]portForwardSpec, error) {
	// Allow multiple entries separated by comma or semicolon, e.g.
	//   ForwardTCP = 25565, 8080:80, 8443:10.0.0.3:443
	//   ForwardUDP = 19132; 25565
	val = strings.ReplaceAll(val, ";", ",")
	var out []portForwardSpec
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		spec, err := parseForwardSpec(part, defaultHost, proto)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	return out, nil
}

// parseForwardSpec accepts (same for TCP and UDP):
//
//	25565                  → 0.0.0.0:25565 -> <AllowedIPs host>:25565
//	25565:25566            → 0.0.0.0:25565 -> <host>:25566
//	25565:10.0.0.3:25565   → 0.0.0.0:25565 -> 10.0.0.3:25565
//	0.0.0.0:25565:10.0.0.3:25565
func parseForwardSpec(spec, defaultHost, proto string) (portForwardSpec, error) {
	label := "ForwardTCP"
	if proto == protoUDP {
		label = "ForwardUDP"
	}
	parts := strings.Split(spec, ":")
	var listenHost string
	var listenPort, destHost string
	var destPort int
	var err error

	switch len(parts) {
	case 1:
		listenHost = "0.0.0.0"
		listenPort = parts[0]
		destHost = defaultHost
		destPort, err = strconv.Atoi(parts[0])
	case 2:
		if net.ParseIP(parts[1]) != nil {
			return portForwardSpec{}, fmt.Errorf("ambiguous %s %q; use listenPort:destHost:destPort", label, spec)
		}
		listenHost = "0.0.0.0"
		listenPort = parts[0]
		destHost = defaultHost
		destPort, err = strconv.Atoi(parts[1])
	case 3:
		listenHost = "0.0.0.0"
		listenPort = parts[0]
		destHost = parts[1]
		destPort, err = strconv.Atoi(parts[2])
	case 4:
		listenHost = parts[0]
		listenPort = parts[1]
		destHost = parts[2]
		destPort, err = strconv.Atoi(parts[3])
	default:
		return portForwardSpec{}, fmt.Errorf("invalid %s %q", label, spec)
	}
	if err != nil {
		return portForwardSpec{}, fmt.Errorf("invalid %s port in %q: %w", label, spec, err)
	}
	lp, err := strconv.Atoi(listenPort)
	if err != nil || lp <= 0 || lp > 65535 || destPort <= 0 || destPort > 65535 {
		return portForwardSpec{}, fmt.Errorf("invalid %s ports in %q", label, spec)
	}
	return portForwardSpec{
		Proto:      proto,
		ListenHost: listenHost,
		ListenPort: lp,
		DestHost:   destHost,
		DestPort:   destPort,
	}, nil
}

func runPeerHooks(hooks []string, env map[string]string, logger *device.Logger, phase string) error {
	for _, cmd := range hooks {
		logger.Verbosef("Peer %s: %s", phase, cmd)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
		c.Env = os.Environ()
		for k, v := range env {
			c.Env = append(c.Env, k+"="+v)
		}
		c.Stdout = os.Stderr
		c.Stderr = os.Stderr
		err := c.Run()
		cancel()
		if err != nil {
			return fmt.Errorf("%s %q: %w", phase, cmd, err)
		}
	}
	return nil
}

func countConfiguredPeers(dev *device.Device) int {
	var ipcBuf bytes.Buffer
	if err := dev.IpcGetOperation(&ipcBuf); err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(ipcBuf.String(), "\n") {
		if strings.HasPrefix(l, "public_key=") {
			n++
		}
	}
	return n
}

func base64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func printStartupInfo(dev *device.Device, logger *device.Logger, iface, confPath string, tcpPort uint16, mcEnabled bool, fwdCount int) {
	var sb strings.Builder
	sb.WriteString("\n┌─────────────────────────────────────────────────┐\n")
	sb.WriteString("│         wireguard-go (TCP + MC mode)            │\n")
	sb.WriteString("├─────────────────────────────────────────────────┤\n")
	fmt.Fprintf(&sb, "│  Interface  : %-33s│\n", iface)
	localPort := "ephemeral"
	if tcpPort != 0 {
		localPort = fmt.Sprintf("%d", tcpPort)
	}
	fmt.Fprintf(&sb, "│  Local TCP  : %-33s│\n", localPort+" (listen)")
	if mcEnabled {
		fmt.Fprintf(&sb, "│  Camouflage : %-33s│\n", "Minecraft (deep)")
	} else {
		fmt.Fprintf(&sb, "│  Camouflage : %-33s│\n", "disabled")
	}
	if confPath != "" {
		fmt.Fprintf(&sb, "│  Config     : %-33s│\n", confPath)
	} else {
		fmt.Fprintf(&sb, "│  Config     : %-33s│\n", "none")
	}
	fmt.Fprintf(&sb, "│  Forwards   : %-33s│\n", fmt.Sprintf("%d", fwdCount))
	sb.WriteString("├─────────────────────────────────────────────────┤\n")

	var ipcBuf bytes.Buffer
	peerCount := 0
	endpoints := make([]string, 0, 4)
	if err := dev.IpcGetOperation(&ipcBuf); err == nil {
		for _, l := range strings.Split(ipcBuf.String(), "\n") {
			if strings.HasPrefix(l, "public_key=") {
				peerCount++
			}
			if strings.HasPrefix(l, "endpoint=") {
				endpoints = append(endpoints, strings.TrimPrefix(l, "endpoint="))
			}
		}
	}
	fmt.Fprintf(&sb, "│  Peers      : %-33s│\n", fmt.Sprintf("%d", peerCount))
	if len(endpoints) == 0 {
		fmt.Fprintf(&sb, "│  Endpoint   : %-33s│\n", "(none)")
	} else {
		for i, ep := range endpoints {
			label := "Endpoint"
			if i > 0 {
				label = "         "
			}
			fmt.Fprintf(&sb, "│  %-10s: %-33s│\n", label, ep)
		}
	}
	sb.WriteString("└─────────────────────────────────────────────────┘\n")
	if peerCount == 0 {
		sb.WriteString("WARNING: no peers active — check [Peer] PublicKey is the REMOTE key.\n")
	}
	fmt.Fprint(os.Stderr, sb.String())
}
