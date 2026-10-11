package store

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
)

const (
	metaRelayPort     = "relay_port"
	metaRelayPrivKey  = "relay_private_key"
	metaRelayPubKey   = "relay_public_key"
	metaRelayHost     = "relay_host"
	metaRelayPublicV4 = "relay_public_v4"

	// DefaultRelayPort is the TCP port Master's cross-subnet fallback relay listens on.
	DefaultRelayPort = 25599
)

// RelayConfig is what Master's relay device needs to run. Port 0 means disabled.
type RelayConfig struct {
	Port       uint16
	PrivateKey string
	PublicKey  string
	Host       string // admin pin
	PublicV4   string // auto-detected public IPv4
}

func (s *Store) ensureRelaySeeds() error {
	if _, err := s.metaGet(metaRelayPrivKey); err == nil {
		return nil
	}
	priv, pub, err := core.GenerateKeyPair()
	if err != nil {
		return err
	}
	for k, v := range map[string]string{
		metaRelayPrivKey: priv,
		metaRelayPubKey:  pub,
	} {
		if err := s.metaSet(k, v); err != nil {
			return err
		}
	}
	if _, err := s.metaGet(metaRelayPort); err != nil {
		return s.metaSet(metaRelayPort, strconv.Itoa(DefaultRelayPort))
	}
	return nil
}

func (s *Store) Relay() RelayConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.relayLocked()
}

func (s *Store) relayLocked() RelayConfig {
	var rc RelayConfig
	if v, err := s.metaGet(metaRelayPort); err == nil {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			rc.Port = uint16(p)
		}
	}
	rc.PrivateKey, _ = s.metaGet(metaRelayPrivKey)
	rc.PublicKey, _ = s.metaGet(metaRelayPubKey)
	rc.Host, _ = s.metaGet(metaRelayHost)
	rc.PublicV4, _ = s.metaGet(metaRelayPublicV4)
	return rc
}

// meshRelayLocked is the public half of the relay for planning, nil when disabled.
func (s *Store) meshRelayLocked() *core.Relay {
	rc := s.relayLocked()
	if rc.Port == 0 || rc.PublicKey == "" || rc.PrivateKey == "" {
		return nil
	}
	host := strings.TrimSpace(rc.Host)
	if host == "" {
		host = strings.TrimSpace(rc.PublicV4)
	}
	return &core.Relay{PublicKey: rc.PublicKey, Port: rc.Port, Host: host}
}

// SetRelayPublicV4 records an auto-detected origin IPv4. When no admin pin is
// set, a change bumps the mesh revision so agents redial the emergency relay.
func (s *Store) SetRelayPublicV4(v4 string) (bool, error) {
	v4 = strings.TrimSpace(v4)
	if v4 == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, _ := s.metaGet(metaRelayPublicV4)
	if cur == v4 {
		return false, nil
	}
	if err := s.metaSet(metaRelayPublicV4, v4); err != nil {
		return false, err
	}
	pin, _ := s.metaGet(metaRelayHost)
	if strings.TrimSpace(pin) != "" {
		return false, nil
	}
	_, err := s.db.Exec(`UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = ?`, metaRevision)
	return err == nil, err
}

func normalizeRelayPort(p int) (string, error) {
	if p < 0 || p > 65535 {
		return "", fmt.Errorf("relayPort must be 0 (disabled) or 1-65535")
	}
	return strconv.Itoa(p), nil
}

func normalizeRelayHost(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if a, err := netip.ParseAddr(v); err == nil {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsMulticast() || a.IsLoopback() {
			return "", fmt.Errorf("relayHost must be a public IP or hostname")
		}
		return a.String(), nil
	}
	if strings.ContainsAny(v, ":/ ") || len(v) > 253 || v == core.RelayEndpointHost {
		return "", fmt.Errorf("relayHost must be a public IP or hostname without a port")
	}
	return v, nil
}
