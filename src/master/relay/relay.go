// Package relay runs Master's cross-subnet fallback: a WireGuard device with no
// OS interface that forwards between nodes of subnets no mother can bridge.
// It only listens while some node actually routes through it.
package relay

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

const syncEvery = 15 * time.Second

type Service struct {
	Store *store.Store

	mu      sync.Mutex
	dev     *device.Device
	port    uint16
	privKey string
	peers   map[string][]string // pubkey hex -> allowed IPs
	sig     string
}

func (s *Service) Run(stop <-chan struct{}) {
	s.Sync()
	t := time.NewTicker(syncEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			s.mu.Lock()
			s.closeLocked()
			s.mu.Unlock()
			return
		case <-t.C:
			s.Sync()
		}
	}
}

// Sync brings the relay device in line with the current mesh plan.
func (s *Service) Sync() {
	rc := s.Store.Relay()
	m := s.Store.Snapshot()
	var want []core.DesiredPeer
	if m.Relay != nil {
		want = core.PlanMesh(&m).RelayPeers()
	}
	settings, err := s.Store.Settings()
	if err == nil && len(settings.TransportJSON) > 0 {
		conn.SetTransportConfigJSON(settings.TransportJSON)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(want) == 0 || rc.Port == 0 {
		s.closeLocked()
		return
	}
	sig, _ := json.Marshal(struct {
		Port  uint16
		Key   string
		Peers []core.DesiredPeer
	}{rc.Port, rc.PrivateKey, want})
	if s.dev != nil && string(sig) == s.sig {
		return
	}
	if err := s.applyLocked(rc, want); err != nil {
		fmt.Fprintf(os.Stderr, "wireguard-go master: relay: %v\n", err)
		s.closeLocked()
		return
	}
	s.sig = string(sig)
}

func (s *Service) applyLocked(rc store.RelayConfig, want []core.DesiredPeer) error {
	fresh := s.dev == nil || s.port != rc.Port || s.privKey != rc.PrivateKey
	if fresh {
		s.closeLocked()
		s.dev = device.NewDevice(newHairpinTUN(), conn.NewTCPBind(), device.NewLogger(device.LogLevelSilent, "relay: "))
		s.peers = map[string][]string{}
	}
	var b bytes.Buffer
	if fresh {
		key, err := b64hex(rc.PrivateKey)
		if err != nil {
			return fmt.Errorf("private key: %w", err)
		}
		fmt.Fprintf(&b, "private_key=%s\nlisten_port=%d\nreplace_peers=true\n", key, rc.Port)
	}
	next := make(map[string][]string, len(want))
	for _, p := range want {
		pub, err := b64hex(p.PublicKey)
		if err != nil {
			continue
		}
		next[pub] = p.AllowedIPs
		if cur, ok := s.peers[pub]; ok && sameList(cur, p.AllowedIPs) {
			continue
		}
		fmt.Fprintf(&b, "public_key=%s\nreplace_allowed_ips=true\n", pub)
		for _, a := range p.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", a)
		}
	}
	for pub := range s.peers {
		if _, ok := next[pub]; !ok {
			fmt.Fprintf(&b, "public_key=%s\nremove=true\n", pub)
		}
	}
	if err := s.dev.IpcSet(b.String()); err != nil {
		return err
	}
	if fresh {
		if err := s.dev.Up(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wireguard-go master: relay listening on :%d (%d peers)\n", rc.Port, len(next))
	}
	s.port, s.privKey, s.peers = rc.Port, rc.PrivateKey, next
	return nil
}

func (s *Service) closeLocked() {
	if s.dev == nil {
		return
	}
	s.dev.Close()
	s.dev, s.peers, s.sig, s.port = nil, nil, "", 0
	fmt.Fprintf(os.Stderr, "wireguard-go master: relay stopped (not needed)\n")
}

func b64hex(k string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("key must be 32 bytes")
	}
	return hex.EncodeToString(raw), nil
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
