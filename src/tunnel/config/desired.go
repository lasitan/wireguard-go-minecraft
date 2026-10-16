package config

import (
	"bufio"
	"bytes"
	"fmt"
	"net/netip"
	"os"
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
	if want := d.Interface.ListenPort; want != 0 && want != dev.NetListenPort() {
		if prev == nil || want != prev.Interface.ListenPort {
			fmt.Fprintf(w, "listen_port=%d\n", want)
		}
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
			expanded, err := ExpandForward(fw)
			if err != nil {
				return nil, fmt.Errorf("forward listen %q: %w", fw.Listen, err)
			}
			fwdPeer.Forwards = append(fwdPeer.Forwards, expanded...)
		}
	}

	fmt.Fprintf(os.Stderr, "lasitan-cluster: hot-applying revision=%d role=%s (peers +%d -%d ~%d)\n",
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
	if want := tunnelRoutes(d); len(want) > 0 || prev != nil {
		var have []netip.Prefix
		if prev != nil && prev.InterfaceName == d.InterfaceName {
			have = tunnelRoutes(prev)
		}
		if add, del := diffRoutes(want, have); len(add)+len(del) > 0 {
			if err := syncIfaceRoutes(iface, add, del, logger); err != nil {
				logger.Errorf("tunnel routes: %v", err)
				fmt.Fprintf(os.Stderr, "lasitan-cluster: warning: tunnel routes: %v\n", err)
			}
		}
	}
	if d.IPForward {
		if err := EnableIPForward(logger); err != nil {
			logger.Verbosef("ip forward: %v", err)
			fmt.Fprintf(os.Stderr, "lasitan-cluster: warning: enable ip forward: %v\n", err)
		}
	}
	return &ConfApplyResult{NetCfg: netCfg, Peers: peers}, nil
}

// ExpandForward turns one rule into one listener spec per port.
// "1000-1002" with dest "2000" is 1000→2000, 1001→2001, 1002→2002.
// "*" is every port 1-65535, forwarded to the same port when dest is "*".
func ExpandForward(fw core.DesiredForward) ([]spec.PortForwardSpec, error) {
	host, listenSpan, err := core.ParseListenExpr(fw.Listen)
	if err != nil {
		return nil, err
	}
	destSpan, err := core.ResolveDest(listenSpan, fw.Dest, fw.DestPort)
	if err != nil {
		return nil, err
	}
	proto := strings.ToLower(fw.Protocol)
	if proto == "" {
		proto = spec.ProtoTCP
	}
	label := strings.TrimSpace(fw.Listen)
	best := listenSpan.Len() > 64
	out := make([]spec.PortForwardSpec, 0, listenSpan.Len())
	for i := 0; i < listenSpan.Len(); i++ {
		out = append(out, spec.PortForwardSpec{
			Proto:      proto,
			ListenHost: host,
			ListenPort: int(listenSpan.Start) + i,
			DestHost:   fw.DestHost,
			DestPort:   int(destSpan.Start) + i,
			StatListen: label,
			BestEffort: best,
		})
	}
	return out, nil
}
