// Package config holds the local bootstrap files each process reads from
// ConfDir: the Master process config and the agent bootstrap.
package config

import (
	"encoding/json"
	"os"
	"time"
)

const (
	AgentFileName  = "wireguard-go-agent.json"
	MasterFileName = "wireguard-go-master.json"
	RoleLockName   = ".role"
	RoleMaster     = "master"
	RoleAgent      = "agent"
)

// AgentBootstrap is the only local file an agent keeps: API URL + key.
// key is enrollToken before join, then replaced by the node API token from Master.
type AgentBootstrap struct {
	MasterURL string `json:"masterUrl"`
	Key       string `json:"key"`

	// Enroll-time hints; Master owns these once the node exists.
	// role "server" makes every client link to this node; endpoint is the
	// public host:port clients dial (Master uses the enroll source IP if empty).
	Role       string `json:"role,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	ListenPort uint16 `json:"listenPort,omitempty"`

	// Deprecated local fields (ignored if present; migrated away on enroll).
	EnrollToken  string `json:"enrollToken,omitempty"`
	NodeToken    string `json:"nodeToken,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
	Interface    string `json:"interface,omitempty"`
}

// MasterConfig is the minimal process bootstrap for Master (path to SQLite + listen/auth).
// Enroll token, address pool, mesh, transport and all agent settings live in
// SQLite under DataDir and are edited from the web UI.
type MasterConfig struct {
	Listen        string `json:"listen"`
	AdminPassword string `json:"adminPassword"`
	DataDir       string `json:"dataDir"` // SQLite mesh.db lives here
	TLSCert       string `json:"tlsCert,omitempty"`
	TLSKey        string `json:"tlsKey,omitempty"`
	// GeoIPDB is an offline country mmdb (GeoLite2-Country / DB-IP Lite).
	// Defaults to <dataDir>/GeoLite2-Country.mmdb or dbip-country-lite.mmdb.
	GeoIPDB string `json:"geoipDb,omitempty"`
	// DisableGeoIPOnline turns off the ip-api.com fallback.
	DisableGeoIPOnline bool `json:"disableGeoipOnline,omitempty"`
	// DisableUpdateCheck stops polling GitHub Releases for new versions.
	DisableUpdateCheck bool `json:"disableUpdateCheck,omitempty"`
}

func LoadJSON[T any](path string, dst *T) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func SaveJSON(path string, v any, mode os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a AgentBootstrap) APIKey() string {
	if a.Key != "" {
		return a.Key
	}
	if a.NodeToken != "" {
		return a.NodeToken
	}
	return a.EnrollToken
}

func (a AgentBootstrap) PollDuration() time.Duration {
	// Local poll interval is obsolete; Master desired config drives polling.
	return 10 * time.Second
}

func (a AgentBootstrap) IfaceName() string {
	return "wg0"
}

// Normalized drops the deprecated fields for persistence.
func (a AgentBootstrap) Normalized() AgentBootstrap {
	return AgentBootstrap{
		MasterURL:  a.MasterURL,
		Key:        a.APIKey(),
		Role:       a.Role,
		Endpoint:   a.Endpoint,
		ListenPort: a.ListenPort,
	}
}
