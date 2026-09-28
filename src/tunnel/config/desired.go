package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/tunnel/spec"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

// ApplyDesiredConfig hot-applies a Master-compiled desired config: only the
// delta against the live device (and prev, the last applied revision) is
// written, so unchanged peers keep their sessions and handshakes. prev is nil
// on the first apply.
func ApplyDesiredConfig(dev *device.Device, logger *device.Logger, iface string, d, prev *core.DesiredConfig) (*ConfApplyResult, error) {
	if d == nil {
		return &ConfApplyResult{}, nil
	}
	var buf bytes.Buffer
	w := &buf

	hexKey, err := base64ToHex(d.Interface.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("privateKey: %w", err)
	}
	fmt.Fprintf(w, "private_key=%s\n", hexKey)
	if prev == nil {
		if d.Interface.ListenPort != 0 {
			fmt.Fprintf(w, "listen_port=%d\n", d.Interface.ListenPort)
		}
	} else if d.Interface.ListenPort != prev.Interface.ListenPort {
		fmt.Fprintf(w, "listen_port=%d\n", d.Interface.ListenPort)
	}

	live, err := readLivePeers(dev)
	if err != nil {
		return nil, fmt.Errorf("read device peers: %w", err)
	}
	prevEp := prevEndpoints(prev)
	wanted := make(map[string]struct{}, len(d.Peers))
	var added, removed, updated int

	var netCfg IfaceNetConfig
	if d.Interface.Address != "" {
		netCfg.addresses = append(netCfg.addresses, d.Interface.Address)
	}
	if d.Interface.MTU > 0 {
		netCfg.mtu = d.Interface.MTU
	}

	var peers []spec.PeerHookConfig
	for _, p := range d.Peers {
		pubHex, err := base64ToHex(p.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("peer publicKey: %w", err)
		}
		wanted[pubHex] = struct{}{}
		cur := live[pubHex]
		var peerBuf bytes.Buffer
		pw := &peerBuf
		// Only re-point the endpoint when Master changed it, so a peer that
		// roamed keeps its live address across unrelated revisions.
		if p.Endpoint != "" && (cur == nil || (cur.endpoint != p.Endpoint && prevEp[p.PublicKey] != p.Endpoint)) {
			fmt.Fprintf(pw, "endpoint=%s\n", p.Endpoint)
		}
		want := make(map[string]struct{}, len(p.AllowedIPs))
		ph := spec.PeerHookConfig{
			Label:        truncateKey(p.PublicKey),
			PublicKeyB64: p.PublicKey,
			PublicKeyHex: pubHex,
			Endpoint:     p.Endpoint,
		}
		for _, cidr := range p.AllowedIPs {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			pfx := canonicalPrefix(cidr)
			want[pfx] = struct{}{}
			if cur == nil {
				fmt.Fprintf(pw, "allowed_ip=%s\n", pfx)
			} else if _, ok := cur.allowed[pfx]; !ok {
				fmt.Fprintf(pw, "allowed_ip=%s\n", pfx)
			}
			ph.HasAllowedIPs = true
			if host, err := firstHostFromCIDR(cidr); err == nil {
				if ph.AllowedIP == "" {
					ph.AllowedIP = host
				}
				ph.AllowedHosts = append(ph.AllowedHosts, host)
			}
		}
		if cur != nil {
			for pfx := range cur.allowed {
				if _, ok := want[pfx]; !ok {
					fmt.Fprintf(pw, "allowed_ip=-%s\n", pfx)
				}
			}
		}
		if p.Keepalive > 0 {
			ph.HasKeepalive = true
		}
		if (cur == nil && p.Keepalive > 0) || (cur != nil && cur.keepalive != p.Keepalive) {
			fmt.Fprintf(pw, "persistent_keepalive_interval=%d\n", p.Keepalive)
		}
		switch {
		case cur == nil:
			added++
			fmt.Fprintf(w, "public_key=%s\n", pubHex)
			buf.Write(peerBuf.Bytes())
		case peerBuf.Len() > 0:
			updated++
			fmt.Fprintf(w, "public_key=%s\n", pubHex)
			buf.Write(peerBuf.Bytes())
		}
		peers = append(peers, ph)
	}
	for pubHex := range live {
		if _, ok := wanted[pubHex]; !ok {
			removed++
			fmt.Fprintf(w, "public_key=%s\nremove=true\n", pubHex)
		}
	}

	// Attach node-level forwards to a synthetic peer hook (or first matching dest).
	if len(d.Forwards) > 0 {
		var fwdPeer *spec.PeerHookConfig
		for i := range peers {
			fwdPeer = &peers[i]
			break
		}
		if fwdPeer == nil {
			peers = append(peers, spec.PeerHookConfig{Label: "forwards"})
			fwdPeer = &peers[len(peers)-1]
		}
		for _, fw := range d.Forwards {
			listenHost, listenPort, err := parseListen(fw.Listen)
			if err != nil {
				return nil, fmt.Errorf("forward listen %q: %w", fw.Listen, err)
			}
			fwSpec := spec.PortForwardSpec{
				Proto:      strings.ToLower(fw.Protocol),
				ListenHost: listenHost,
				ListenPort: int(listenPort),
				DestHost:   fw.DestHost,
				DestPort:   int(fw.DestPort),
			}
			if fwSpec.Proto == "" {
				fwSpec.Proto = spec.ProtoTCP
			}
			fwdPeer.Forwards = append(fwdPeer.Forwards, fwSpec)
		}
	}

	fmt.Fprintf(os.Stderr, "wireguard-go: hot-applying revision=%d role=%s (peers +%d -%d ~%d)\n",
		d.Revision, d.Role, added, removed, updated)
	if err := dev.IpcSetOperation(bufio.NewReader(&buf)); err != nil {
		return nil, fmt.Errorf("apply desired uapi: %w", err)
	}
	if !sameIfaceNet(prev, d) {
		if prev != nil && prev.Interface.Address != "" && prev.Interface.Address != d.Interface.Address {
			removeIfaceAddress(iface, prev.Interface.Address, logger)
		}
		if err := applyIfaceNetConfig(iface, netCfg, logger); err != nil {
			return nil, err
		}
	}
	if d.IPForward {
		if err := EnableIPForward(logger); err != nil {
			logger.Verbosef("ip forward: %v", err)
			fmt.Fprintf(os.Stderr, "wireguard-go: warning: enable ip forward: %v\n", err)
		}
	}
	return &ConfApplyResult{NetCfg: netCfg, Peers: peers}, nil
}

func parseListen(listen string) (host string, port uint16, err error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "", 0, fmt.Errorf("empty")
	}
	if strings.Contains(listen, ":") {
		host, portStr, ok := strings.Cut(listen, ":")
		if !ok {
			return "", 0, fmt.Errorf("invalid listen %q", listen)
		}
		// handle :3389
		if host == "" {
			host = "0.0.0.0"
		}
		p, e := strconv.ParseUint(portStr, 10, 16)
		if e != nil || p == 0 {
			return "", 0, fmt.Errorf("invalid port in %q", listen)
		}
		return host, uint16(p), nil
	}
	p, e := strconv.ParseUint(listen, 10, 16)
	if e != nil || p == 0 {
		return "", 0, fmt.Errorf("invalid port %q", listen)
	}
	return "0.0.0.0", uint16(p), nil
}
