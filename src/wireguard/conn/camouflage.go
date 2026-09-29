/* SPDX-License-Identifier: MIT */

package conn

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"
)

const (
	camoProfileNone      = "none"
	camoProfileMinecraft = "minecraft"
	camoProfileSource    = "source"
	camoProfileTerraria  = "terraria"
	camoProfileSteam     = "steam"
	camoProfileBedrock   = "bedrock"
	camoProfileFiveM     = "fivem"
)

type camoSharedConfig struct {
	profile       string
	deep          bool
	timeout       time.Duration
	loginUsername string
	pluginChannel string
	pluginSecret  string
	rejectMessage string
	serverName    string
	secure        bool
}

type camoConfigFile struct {
	Profile            string `json:"profile"`
	Deep               *bool  `json:"deep"`
	Secure             *bool  `json:"secure"`
	HandshakeTimeout   string `json:"handshakeTimeout"`
	LoginUsername      string `json:"loginUsername"`
	LoginPluginChannel string `json:"loginPluginChannel"`
	LoginPluginSecret  string `json:"loginPluginSecret"`
	RejectMessage      string `json:"rejectMessage"`
	ServerName         string `json:"serverName"`
}

func normalizeCamoProfile(p string) string {
	p = strings.TrimSpace(strings.ToLower(p))
	switch p {
	case "", "off", "disabled", camoProfileNone:
		return camoProfileNone
	case "mc", "minecraft", "minecraft-java":
		return camoProfileMinecraft
	case "source", "cs2", "csgo", "goldsrc":
		return camoProfileSource
	case "terraria":
		return camoProfileTerraria
	case "steam", "valheim", "rust", "ark":
		return camoProfileSteam
	case "bedrock", "minecraft-bedrock":
		return camoProfileBedrock
	case "fivem", "gta":
		return camoProfileFiveM
	default:
		return p
	}
}

func loadCamouflageConfig(fileCfg transportConfigFile) (camoSharedConfig, mcCamouflageConfig, error) {
	mc, err := loadMCCamouflageConfig(fileCfg.MC)
	if err != nil {
		return camoSharedConfig{}, mcCamouflageConfig{}, err
	}
	cf := fileCfg.Camouflage
	profile := normalizeCamoProfile(cf.Profile)
	if cf.Profile == "" {
		if mc.enabled {
			profile = camoProfileMinecraft
		} else {
			profile = camoProfileNone
		}
	}
	deep := mc.deep
	if cf.Deep != nil {
		deep = *cf.Deep
	}
	timeout := mc.timeout
	if strings.TrimSpace(cf.HandshakeTimeout) != "" {
		timeout = parseDurationWithDefault(cf.HandshakeTimeout, defaultMCHandshakeTime)
	}
	secret := strings.TrimSpace(cf.LoginPluginSecret)
	if secret == "" {
		secret = mc.pluginSecret
	}
	channel := strings.TrimSpace(cf.LoginPluginChannel)
	if channel == "" {
		channel = mc.pluginChannel
	}
	user := strings.TrimSpace(cf.LoginUsername)
	if user == "" {
		user = mc.loginUsername
	}
	reject := strings.TrimSpace(cf.RejectMessage)
	if reject == "" {
		reject = mc.rejectMessage
	}
	name := strings.TrimSpace(cf.ServerName)
	if name == "" {
		name = "Dedicated Server"
	}
	shared := camoSharedConfig{
		profile:       profile,
		deep:          deep,
		timeout:       timeout,
		loginUsername: user,
		pluginChannel: channel,
		pluginSecret:  secret,
		rejectMessage: reject,
		serverName:    name,
		// Omit → false so pre-secure agents keep working; new Masters ship
		// "secure": true explicitly in DefaultTransportJSON.
		secure: cf.Secure != nil && *cf.Secure,
	}
	if profile == camoProfileMinecraft {
		mc.enabled = true
		mc.deep = deep
		mc.timeout = timeout
		mc.pluginSecret = secret
		mc.pluginChannel = channel
		mc.loginUsername = user
		mc.rejectMessage = reject
	} else if profile != camoProfileNone {
		mc.enabled = false
	}
	return shared, mc, nil
}

func (b *TCPBind) getCamo() camoSharedConfig {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.camoShared
}

// performCamouflageClient runs the profile handshake and returns the conn to
// carry frames on (stream-encrypted when secure camouflage is on).
func (b *TCPBind) performCamouflageClient(conn net.Conn, dst netip.AddrPort) (net.Conn, error) {
	cfg := b.getCamo()
	var err error
	switch cfg.profile {
	case camoProfileNone:
		return conn, nil
	case camoProfileMinecraft:
		return b.performMCCamouflageClient(conn, dst)
	case camoProfileSource:
		err = b.performSourceCamouflageClient(conn, cfg)
	case camoProfileTerraria:
		err = b.performTerrariaCamouflageClient(conn, cfg)
	case camoProfileSteam:
		err = b.performSteamCamouflageClient(conn, cfg)
	case camoProfileBedrock:
		err = b.performBedrockCamouflageClient(conn, cfg)
	case camoProfileFiveM:
		err = b.performFiveMCamouflageClient(conn, cfg)
	default:
		return nil, fmt.Errorf("unknown camouflage profile %q", cfg.profile)
	}
	if err != nil || !cfg.secure {
		return conn, err
	}
	return b.secureGenericClient(conn, cfg)
}

func (b *TCPBind) performCamouflageServer(conn net.Conn) (net.Conn, bool, error) {
	cfg := b.getCamo()
	var err error
	switch cfg.profile {
	case camoProfileNone:
		return conn, false, nil
	case camoProfileMinecraft:
		return b.performMCCamouflageServer(conn)
	case camoProfileSource:
		err = b.performSourceCamouflageServer(conn, cfg)
	case camoProfileTerraria:
		err = b.performTerrariaCamouflageServer(conn, cfg)
	case camoProfileSteam:
		err = b.performSteamCamouflageServer(conn, cfg)
	case camoProfileBedrock:
		err = b.performBedrockCamouflageServer(conn, cfg)
	case camoProfileFiveM:
		err = b.performFiveMCamouflageServer(conn, cfg)
	default:
		return nil, false, fmt.Errorf("unknown camouflage profile %q", cfg.profile)
	}
	if err != nil || !cfg.secure {
		return conn, false, err
	}
	return b.secureGenericServer(conn, cfg)
}

func camoAuthClient(conn net.Conn, secret string, challenge []byte) error {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(challenge)
	sum := mac.Sum(nil)
	// No ASCII marker — looks like a continuation blob, not a VPN tag.
	return writeAll(conn, sum)
}

func camoAuthServer(conn net.Conn, secret string, challenge []byte) error {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("camo auth read: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(challenge)
	if !hmac.Equal(buf, mac.Sum(nil)) {
		return fmt.Errorf("camo auth rejected")
	}
	return nil
}
