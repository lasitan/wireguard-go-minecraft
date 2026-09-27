package spec

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"time"

	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

// PeerHookConfig holds per-peer extras that are not part of WireGuard UAPI.
type PeerHookConfig struct {
	Label         string // truncated public key for logs
	PublicKeyB64  string // original base64 public key
	PublicKeyHex  string
	Endpoint      string // Endpoint= from conf (empty if unset)
	AllowedIP     string // first usable host from AllowedIPs (for ForwardTCP/UDP shorthand)
	AllowedHosts  []string
	HasAllowedIPs bool
	HasKeepalive  bool
	NatClient     bool // effective NatClient (after auto-detect)
	NatClientSet  bool // NatClient= was present in conf
	Forwards      []PortForwardSpec
	OnUp          []string
	OnDown        []string
}

// NatGatewayResult is populated when dual-server / TO NAT is configured.
type NatGatewayResult struct {
	ToNATClient    bool // C-side Interface ToNAT=true
	ServerMode     bool // B-side: ListenPort + upstream — ready for ToNAT clients
	NatUpstreamB64 string
	ListenPort     uint16
	ClientHosts    []string // explicit NatClient=true hosts (optional seed)
	ClientKeyHex   []string // explicit NatClient=true keys
	UpstreamAddr   netip.AddrPort
	VPNPrefixes    []netip.Prefix
	// PeersByKeyHex maps peer public key hex → AllowedIPs hosts (for runtime ToNAT)
	PeersByKeyHex map[string][]string
}

// RunPeerHooks runs OnUp/OnDown shell commands with the peer env appended.
func RunPeerHooks(hooks []string, env map[string]string, logger *device.Logger, phase string) error {
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
