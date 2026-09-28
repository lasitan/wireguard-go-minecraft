package store

import (
	"fmt"
	"strconv"

	"golang.zx2c4.com/wireguard/src/core"
)

const (
	metaRelayPort    = "relay_port"
	metaRelayPrivKey = "relay_private_key"
	metaRelayPubKey  = "relay_public_key"

	// DefaultRelayPort is the TCP port Master's cross-subnet fallback relay listens on.
	DefaultRelayPort = 25599
)

// RelayConfig is what Master's relay device needs to run. Port 0 means disabled.
type RelayConfig struct {
	Port       uint16
	PrivateKey string
	PublicKey  string
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
	return rc
}

// meshRelayLocked is the public half of the relay for planning, nil when disabled.
func (s *Store) meshRelayLocked() *core.Relay {
	rc := s.relayLocked()
	if rc.Port == 0 || rc.PublicKey == "" || rc.PrivateKey == "" {
		return nil
	}
	return &core.Relay{PublicKey: rc.PublicKey, Port: rc.Port}
}

func normalizeRelayPort(p int) (string, error) {
	if p < 0 || p > 65535 {
		return "", fmt.Errorf("relayPort must be 0 (disabled) or 1-65535")
	}
	return strconv.Itoa(p), nil
}
