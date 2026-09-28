package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// DefaultVPNSubnet is the first-run pool that enroll allocates node addresses from.
const DefaultVPNSubnet = "10.10.0.0/24"

// DefaultServerListenPort is the TCP port a server node listens on when enroll omits one.
const DefaultServerListenPort uint16 = 25590

// SettingsPatch updates Master-wide settings edited from the web UI.
// Nil fields are left unchanged.
type SettingsPatch struct {
	EnrollToken   *string          `json:"enrollToken,omitempty"`
	VPNSubnet     *string          `json:"vpnSubnet,omitempty"`
	TransportJSON *json.RawMessage `json:"transportJson,omitempty"`
	RelayPort     *int             `json:"relayPort,omitempty"`
}

// NewEnrollToken returns a random URL-safe token for agents to join with.
func NewEnrollToken() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func normalizeEnrollToken(v string) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) < 8 {
		return "", fmt.Errorf("enrollToken must be at least 8 characters")
	}
	if len(v) > 128 || strings.ContainsAny(v, " \t\r\n\"") {
		return "", fmt.Errorf("enrollToken must be at most 128 characters without spaces or quotes")
	}
	return v, nil
}

func normalizePool(v string) (string, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(v))
	if err != nil {
		return "", fmt.Errorf("vpnSubnet: invalid CIDR")
	}
	if !p.Addr().Is4() {
		return "", fmt.Errorf("vpnSubnet must be IPv4")
	}
	if p.Bits() < 8 || p.Bits() > 30 {
		return "", fmt.Errorf("vpnSubnet prefix must be between /8 and /30")
	}
	return p.Masked().String(), nil
}

// UpdateSettings validates and stores a settings patch. Changing the enroll
// token does not affect agents that already joined (they use node tokens).
func (s *Store) UpdateSettings(p SettingsPatch) error {
	updates := map[string]string{}
	if p.EnrollToken != nil {
		v, err := normalizeEnrollToken(*p.EnrollToken)
		if err != nil {
			return err
		}
		updates[metaEnrollToken] = v
	}
	if p.VPNSubnet != nil {
		v, err := normalizePool(*p.VPNSubnet)
		if err != nil {
			return err
		}
		updates[metaVPNSubnet] = v
	}
	if p.TransportJSON != nil {
		if len(*p.TransportJSON) == 0 {
			return fmt.Errorf("transportJson must not be empty")
		}
		if !json.Valid(*p.TransportJSON) {
			return fmt.Errorf("transportJson: invalid JSON")
		}
		updates[metaTransportJSON] = string(*p.TransportJSON)
	}
	if p.RelayPort != nil {
		v, err := normalizeRelayPort(*p.RelayPort)
		if err != nil {
			return err
		}
		updates[metaRelayPort] = v
	}
	if len(updates) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range updates {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	if _, ok := updates[metaRelayPort]; ok {
		// Agents' relay endpoints change with it.
		if _, err := tx.Exec(`UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = ?`, metaRevision); err != nil {
			return err
		}
	}
	return tx.Commit()
}
