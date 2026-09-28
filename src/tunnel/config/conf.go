/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package config

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/src/tunnel/spec"

	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

type IfaceNetConfig struct {
	addresses []string
	mtu       int
}

type ConfApplyResult struct {
	NetCfg IfaceNetConfig
	Peers  []spec.PeerHookConfig
	Nat    spec.NatGatewayResult
}

func (c IfaceNetConfig) Addresses() []string { return c.addresses }
func (c IfaceNetConfig) MTU() int            { return c.mtu }

// applyWGConf reads /etc/lasitan-cluster/<iface>.conf, applies UAPI settings,
// configures Address/MTU, starts ForwardTCP proxies, and runs peer OnUp scripts.
func ApplyWGConf(dev *device.Device, logger *device.Logger, iface string) (*ConfApplyResult, error) {
	confPath := filepath.Join(config.ConfDir(), iface+".conf")
	f, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfApplyResult{}, nil
		}
		return nil, fmt.Errorf("open %s: %w", confPath, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := &buf
	expectedPeers := 0
	var netCfg IfaceNetConfig
	var peers []spec.PeerHookConfig
	var cur *spec.PeerHookConfig
	var toNATClient bool
	var natUpstreamB64 string
	var listenPort uint16

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
				cur = &spec.PeerHookConfig{}
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
				p, err := strconv.ParseUint(val, 10, 16)
				if err != nil || p == 0 {
					return nil, fmt.Errorf("invalid ListenPort %q", val)
				}
				listenPort = uint16(p)
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
			case "tonat":
				v := strings.ToLower(strings.TrimSpace(val))
				switch v {
				case "true", "yes", "1", "on":
					toNATClient = true
				case "false", "no", "0", "off", "":
					toNATClient = false
				default:
					return nil, fmt.Errorf("invalid ToNAT %q (want true/false; put B address in [Peer] Endpoint)", val)
				}
			case "natupstream":
				if val == "" {
					return nil, fmt.Errorf("NatUpstream must not be empty")
				}
				if _, err := base64ToHex(val); err != nil {
					return nil, fmt.Errorf("NatUpstream: %w", err)
				}
				natUpstreamB64 = val
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
				cur.PublicKeyB64 = val
				cur.PublicKeyHex = hexKey
				if len(val) > 8 {
					cur.Label = val[:4] + "…" + val[len(val)-4:]
				} else {
					cur.Label = val
				}
			case "presharedkey":
				hexKey, err := base64ToHex(val)
				if err != nil {
					return nil, fmt.Errorf("PresharedKey: %w", err)
				}
				fmt.Fprintf(w, "preshared_key=%s\n", hexKey)
			case "endpoint":
				cur.Endpoint = val
				fmt.Fprintf(w, "endpoint=%s\n", val)
			case "allowedips":
				for _, cidr := range strings.Split(val, ",") {
					cidr = strings.TrimSpace(cidr)
					if cidr == "" {
						continue
					}
					fmt.Fprintf(w, "allowed_ip=%s\n", cidr)
					cur.HasAllowedIPs = true
					if host, err := firstHostFromCIDR(cidr); err == nil {
						if cur.AllowedIP == "" {
							cur.AllowedIP = host
						}
						cur.AllowedHosts = append(cur.AllowedHosts, host)
					}
				}
			case "persistentkeepalive":
				fmt.Fprintf(w, "persistent_keepalive_interval=%s\n", val)
				cur.HasKeepalive = true
			case "natclient":
				v := strings.ToLower(val)
				switch v {
				case "true", "yes", "1", "on":
					cur.NatClient = true
					cur.NatClientSet = true
				case "false", "no", "0", "off":
					cur.NatClient = false
					cur.NatClientSet = true
				case "":
					// ignore
				default:
					return nil, fmt.Errorf("invalid NatClient %q", val)
				}
			case "forwardtcp":
				specs, err := parseForwardList(val, cur.AllowedIP, spec.ProtoTCP)
				if err != nil {
					return nil, fmt.Errorf("ForwardTCP: %w", err)
				}
				cur.Forwards = append(cur.Forwards, specs...)
			case "forwardudp":
				specs, err := parseForwardList(val, cur.AllowedIP, spec.ProtoUDP)
				if err != nil {
					return nil, fmt.Errorf("ForwardUDP: %w", err)
				}
				cur.Forwards = append(cur.Forwards, specs...)
			case "onup", "postup":
				if val != "" {
					cur.OnUp = append(cur.OnUp, val)
				}
			case "ondown", "predown":
				if val != "" {
					cur.OnDown = append(cur.OnDown, val)
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
		resolved := make([]spec.PortForwardSpec, 0, len(p.Forwards))
		for _, fw := range p.Forwards {
			if fw.DestHost == "" {
				if p.AllowedIP == "" {
					return nil, fmt.Errorf("peer %s: Forward%s needs AllowedIPs host or explicit dest IP", p.Label, strings.ToUpper(fw.Proto))
				}
				fw.DestHost = p.AllowedIP
			}
			key := fw.Proto + "/" + fw.ListenAddr()
			if prev, ok := seenListen[key]; ok {
				return nil, fmt.Errorf("duplicate Forward%s listen %s (peer %s and %s)", strings.ToUpper(fw.Proto), fw.ListenAddr(), prev, p.Label)
			}
			seenListen[key] = p.Label
			resolved = append(resolved, fw)
		}
		p.Forwards = resolved
	}

	nat, peers, err := buildNatGatewayResult(toNATClient, natUpstreamB64, listenPort, netCfg, peers)
	if err != nil {
		return nil, err
	}

	if buf.Len() > 0 {
		fmt.Fprintf(os.Stderr, "lasitan-cluster: applying UAPI (bind TCP ListenPort)…\n")
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

	fmt.Fprintf(os.Stderr, "lasitan-cluster: configuring interface %s (mtu/address)…\n", iface)
	if err := applyIfaceNetConfig(iface, netCfg, logger); err != nil {
		return nil, err
	}

	logger.Verbosef("Applied config from %s", confPath)
	return &ConfApplyResult{NetCfg: netCfg, Peers: peers, Nat: nat}, nil
}

func buildNatGatewayResult(toNATClient bool, natUpstreamB64 string, listenPort uint16, netCfg IfaceNetConfig, peers []spec.PeerHookConfig) (spec.NatGatewayResult, []spec.PeerHookConfig, error) {
	var nat spec.NatGatewayResult
	nat.ToNATClient = toNATClient
	nat.NatUpstreamB64 = natUpstreamB64
	nat.ListenPort = listenPort

	for _, a := range netCfg.addresses {
		prefix, err := netip.ParsePrefix(a)
		if err != nil {
			if ip, err2 := netip.ParseAddr(a); err2 == nil {
				if ip.Is4() {
					prefix = netip.PrefixFrom(ip, 32)
				} else {
					prefix = netip.PrefixFrom(ip, 128)
				}
			} else {
				continue
			}
		}
		nat.VPNPrefixes = append(nat.VPNPrefixes, prefix)
	}

	// C-side: ToNAT=true is a mode flag; [Peer] Endpoint dials B as usual.
	if toNATClient {
		if len(peers) != 1 {
			return nat, peers, fmt.Errorf("ToNAT=true requires exactly one [Peer] (the NAT gateway), got %d", len(peers))
		}
		p := &peers[0]
		if p.PublicKeyHex == "" {
			return nat, peers, fmt.Errorf("ToNAT=true requires [Peer] PublicKey of the NAT gateway")
		}
		if p.Endpoint == "" {
			return nat, peers, fmt.Errorf("ToNAT=true requires [Peer] Endpoint of the NAT gateway (e.g. 10.0.0.2:25565)")
		}
		fmt.Fprintf(os.Stderr, "lasitan-cluster: ToNAT client via peer %s Endpoint %s\n", p.Label, p.Endpoint)
	}

	// Resolve upstream: explicit NatUpstream, else the unique peer that has Endpoint
	// while this node also listens (dual-server B).
	var up *spec.PeerHookConfig
	if natUpstreamB64 != "" {
		upHex, err := base64ToHex(natUpstreamB64)
		if err != nil {
			return nat, peers, fmt.Errorf("NatUpstream: %w", err)
		}
		for i := range peers {
			if peers[i].PublicKeyHex == upHex || peers[i].PublicKeyB64 == natUpstreamB64 {
				up = &peers[i]
				break
			}
		}
		if up == nil {
			return nat, peers, fmt.Errorf("NatUpstream %s does not match any [Peer] PublicKey", truncateKey(natUpstreamB64))
		}
	} else if listenPort != 0 {
		var endpointPeers []*spec.PeerHookConfig
		for i := range peers {
			if peers[i].Endpoint != "" {
				endpointPeers = append(endpointPeers, &peers[i])
			}
		}
		if len(endpointPeers) == 1 {
			up = endpointPeers[0]
			nat.NatUpstreamB64 = up.PublicKeyB64
		}
	}
	if up != nil {
		if up.Endpoint == "" {
			return nat, peers, fmt.Errorf("upstream peer %s must have Endpoint", up.Label)
		}
		addrPort, err := resolveEndpointAddrPort(up.Endpoint)
		if err != nil {
			return nat, peers, fmt.Errorf("upstream Endpoint %q: %w", up.Endpoint, err)
		}
		nat.UpstreamAddr = addrPort
	}

	// Conf-level NatClient=true still honored; otherwise clients are learned at
	// runtime when an inbound ToNAT session completes WG handshake.
	for i := range peers {
		p := &peers[i]
		if up != nil && (p.PublicKeyHex == up.PublicKeyHex || p.PublicKeyB64 == up.PublicKeyB64) {
			if p.NatClientSet && p.NatClient {
				return nat, peers, fmt.Errorf("upstream peer %s cannot be NatClient", p.Label)
			}
			continue
		}
		if p.NatClientSet && p.NatClient {
			if p.PublicKeyHex == "" {
				return nat, peers, fmt.Errorf("peer %s: NatClient requires PublicKey", p.Label)
			}
			if len(p.AllowedHosts) == 0 {
				return nat, peers, fmt.Errorf("peer %s: NatClient requires single-host AllowedIPs (/32 or /128)", p.Label)
			}
			nat.ClientHosts = append(nat.ClientHosts, p.AllowedHosts...)
			nat.ClientKeyHex = append(nat.ClientKeyHex, p.PublicKeyHex)
		}
	}

	if listenPort != 0 && nat.UpstreamAddr.IsValid() {
		nat.ServerMode = true
		nat.PeersByKeyHex = make(map[string][]string)
		for i := range peers {
			p := &peers[i]
			if up != nil && p.PublicKeyHex == up.PublicKeyHex {
				continue
			}
			if p.PublicKeyHex != "" && len(p.AllowedHosts) > 0 {
				nat.PeersByKeyHex[p.PublicKeyHex] = append([]string{}, p.AllowedHosts...)
			}
		}
	}

	return nat, peers, nil
}

func truncateKey(b64 string) string {
	if len(b64) > 8 {
		return b64[:4] + "…" + b64[len(b64)-4:]
	}
	return b64
}

func resolveEndpointAddrPort(endpoint string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(endpoint); err == nil {
		return ap, nil
	}
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return netip.AddrPort{}, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid port: %w", err)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
		}
	}
	return netip.AddrPort{}, fmt.Errorf("no usable address for %q", host)
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

func parseForwardList(val, defaultHost, proto string) ([]spec.PortForwardSpec, error) {
	// Allow multiple entries separated by comma or semicolon, e.g.
	//   ForwardTCP = 25565, 8080:80, 8443:10.0.0.3:443
	//   ForwardUDP = 19132; 25565
	val = strings.ReplaceAll(val, ";", ",")
	var out []spec.PortForwardSpec
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fwSpec, err := parseForwardSpec(part, defaultHost, proto)
		if err != nil {
			return nil, err
		}
		out = append(out, fwSpec)
	}
	return out, nil
}

// parseForwardSpec accepts (same for TCP and UDP):
//
//	25565                  → 0.0.0.0:25565 -> <AllowedIPs host>:25565
//	25565:25566            → 0.0.0.0:25565 -> <host>:25566
//	25565:10.0.0.3:25565   → 0.0.0.0:25565 -> 10.0.0.3:25565
//	0.0.0.0:25565:10.0.0.3:25565
func parseForwardSpec(raw, defaultHost, proto string) (spec.PortForwardSpec, error) {
	label := "ForwardTCP"
	if proto == spec.ProtoUDP {
		label = "ForwardUDP"
	}
	parts := strings.Split(raw, ":")
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
			return spec.PortForwardSpec{}, fmt.Errorf("ambiguous %s %q; use listenPort:destHost:destPort", label, raw)
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
		return spec.PortForwardSpec{}, fmt.Errorf("invalid %s %q", label, raw)
	}
	if err != nil {
		return spec.PortForwardSpec{}, fmt.Errorf("invalid %s port in %q: %w", label, raw, err)
	}
	lp, err := strconv.Atoi(listenPort)
	if err != nil || lp <= 0 || lp > 65535 || destPort <= 0 || destPort > 65535 {
		return spec.PortForwardSpec{}, fmt.Errorf("invalid %s ports in %q", label, raw)
	}
	return spec.PortForwardSpec{
		Proto:      proto,
		ListenHost: listenHost,
		ListenPort: lp,
		DestHost:   destHost,
		DestPort:   destPort,
	}, nil
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

func PrintStartupInfo(dev *device.Device, logger *device.Logger, iface, confPath string, tcpPort uint16, mcEnabled bool, fwdCount int) {
	var sb strings.Builder
	sb.WriteString("\n┌─────────────────────────────────────────────────┐\n")
	sb.WriteString("│            Lasitan-Cluster (TCP+MC)             │\n")
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
