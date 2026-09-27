package tunnel

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/meshcfg"
)

// applyDesiredConfig applies a Master-compiled desired config via UAPI + iface net + returns peers for forwards.
func ApplyDesiredConfig(dev *device.Device, logger *device.Logger, iface string, d *meshcfg.DesiredConfig) (*ConfApplyResult, error) {
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
	if d.Interface.ListenPort != 0 {
		fmt.Fprintf(w, "listen_port=%d\n", d.Interface.ListenPort)
	}
	fmt.Fprintf(w, "replace_peers=true\n")

	var netCfg IfaceNetConfig
	if d.Interface.Address != "" {
		netCfg.addresses = append(netCfg.addresses, d.Interface.Address)
	}
	if d.Interface.MTU > 0 {
		netCfg.mtu = d.Interface.MTU
	}

	var peers []PeerHookConfig
	for _, p := range d.Peers {
		pubHex, err := base64ToHex(p.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("peer publicKey: %w", err)
		}
		fmt.Fprintf(w, "public_key=%s\n", pubHex)
		if p.Endpoint != "" {
			fmt.Fprintf(w, "endpoint=%s\n", p.Endpoint)
		}
		ph := PeerHookConfig{
			label:        truncateKey(p.PublicKey),
			publicKeyB64: p.PublicKey,
			publicKeyHex: pubHex,
			endpoint:     p.Endpoint,
		}
		for _, cidr := range p.AllowedIPs {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			fmt.Fprintf(w, "allowed_ip=%s\n", cidr)
			ph.hasAllowedIPs = true
			if host, err := firstHostFromCIDR(cidr); err == nil {
				if ph.allowedIP == "" {
					ph.allowedIP = host
				}
				ph.allowedHosts = append(ph.allowedHosts, host)
			}
		}
		if p.Keepalive > 0 {
			fmt.Fprintf(w, "persistent_keepalive_interval=%d\n", p.Keepalive)
			ph.hasKeepalive = true
		}
		peers = append(peers, ph)
	}

	// Attach node-level forwards to a synthetic peer hook (or first matching dest).
	if len(d.Forwards) > 0 {
		var fwdPeer *PeerHookConfig
		for i := range peers {
			fwdPeer = &peers[i]
			break
		}
		if fwdPeer == nil {
			peers = append(peers, PeerHookConfig{label: "forwards"})
			fwdPeer = &peers[len(peers)-1]
		}
		for _, fw := range d.Forwards {
			listenHost, listenPort, err := parseListen(fw.Listen)
			if err != nil {
				return nil, fmt.Errorf("forward listen %q: %w", fw.Listen, err)
			}
			spec := PortForwardSpec{
				Proto:      strings.ToLower(fw.Protocol),
				ListenHost: listenHost,
				ListenPort: int(listenPort),
				DestHost:   fw.DestHost,
				DestPort:   int(fw.DestPort),
			}
			if spec.Proto == "" {
				spec.Proto = protoTCP
			}
			fwdPeer.forwards = append(fwdPeer.forwards, spec)
		}
	}

	fmt.Fprintf(os.Stderr, "wireguard-go: applying desired config revision=%d role=%s…\n", d.Revision, d.Role)
	if err := dev.IpcSetOperation(bufio.NewReader(&buf)); err != nil {
		return nil, fmt.Errorf("apply desired uapi: %w", err)
	}
	if err := applyIfaceNetConfig(iface, netCfg, logger); err != nil {
		return nil, err
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
