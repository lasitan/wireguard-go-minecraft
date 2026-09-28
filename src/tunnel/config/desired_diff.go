package config

import (
	"bytes"
	"net/netip"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

// livePeer is a peer's current device state as reported by UAPI get.
type livePeer struct {
	endpoint  string
	keepalive int
	allowed   map[string]struct{}
}

// readLivePeers returns the device's peers keyed by hex public key.
func readLivePeers(dev *device.Device) (map[string]*livePeer, error) {
	var buf bytes.Buffer
	if err := dev.IpcGetOperation(&buf); err != nil {
		return nil, err
	}
	peers := make(map[string]*livePeer)
	var cur *livePeer
	for _, line := range strings.Split(buf.String(), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "public_key":
			cur = &livePeer{allowed: make(map[string]struct{})}
			peers[val] = cur
		case "endpoint":
			if cur != nil {
				cur.endpoint = val
			}
		case "persistent_keepalive_interval":
			if cur != nil {
				cur.keepalive, _ = strconv.Atoi(val)
			}
		case "allowed_ip":
			if cur != nil {
				cur.allowed[val] = struct{}{}
			}
		}
	}
	return peers, nil
}

// canonicalPrefix matches the masked form UAPI get prints; bare IPs become host routes.
func canonicalPrefix(s string) string {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String()
	}
	if ip, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(ip, ip.BitLen()).String()
	}
	return s
}

func prevEndpoints(prev *core.DesiredConfig) map[string]string {
	out := make(map[string]string)
	if prev == nil {
		return out
	}
	for _, p := range prev.Peers {
		out[p.PublicKey] = p.Endpoint
	}
	return out
}

// sameIfaceNet reports whether address/MTU are unchanged since prev, so the
// OS interface (and connections bound to its address) can be left alone.
func sameIfaceNet(prev, d *core.DesiredConfig) bool {
	return prev != nil &&
		prev.InterfaceName == d.InterfaceName &&
		prev.Interface.Address == d.Interface.Address &&
		prev.Interface.MTU == d.Interface.MTU
}
