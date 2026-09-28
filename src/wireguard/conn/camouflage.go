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
	camoProfileNone       = "none"
	camoProfileMinecraft  = "minecraft"
	camoProfileSource     = "source"
	camoProfileTerraria   = "terraria"
	camoProfileSteam      = "steam"
	camoProfileBedrock    = "bedrock"
	camoProfileFiveM      = "fivem"
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
}

type camoConfigFile struct {
	Profile            string `json:"profile"`
	Deep               *bool  `json:"deep"`
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

func (b *TCPBind) performCamouflageClient(conn net.Conn, dst netip.AddrPort) error {
	cfg := b.getCamo()
	switch cfg.profile {
	case camoProfileNone:
		return nil
	case camoProfileMinecraft:
		return b.performMCCamouflageClient(conn, dst)
	case camoProfileSource:
		return b.performSourceCamouflageClient(conn, cfg)
	case camoProfileTerraria:
		return b.performTerrariaCamouflageClient(conn, cfg)
	case camoProfileSteam:
		return b.performSteamCamouflageClient(conn, cfg)
	case camoProfileBedrock:
		return b.performBedrockCamouflageClient(conn, cfg)
	case camoProfileFiveM:
		return b.performFiveMCamouflageClient(conn, cfg)
	default:
		return fmt.Errorf("unknown camouflage profile %q", cfg.profile)
	}
}

func (b *TCPBind) performCamouflageServer(conn net.Conn) (toNAT bool, err error) {
	cfg := b.getCamo()
	switch cfg.profile {
	case camoProfileNone:
		return false, nil
	case camoProfileMinecraft:
		return b.performMCCamouflageServer(conn)
	case camoProfileSource:
		return false, b.performSourceCamouflageServer(conn, cfg)
	case camoProfileTerraria:
		return false, b.performTerrariaCamouflageServer(conn, cfg)
	case camoProfileSteam:
		return false, b.performSteamCamouflageServer(conn, cfg)
	case camoProfileBedrock:
		return false, b.performBedrockCamouflageServer(conn, cfg)
	case camoProfileFiveM:
		return false, b.performFiveMCamouflageServer(conn, cfg)
	default:
		return false, fmt.Errorf("unknown camouflage profile %q", cfg.profile)
	}
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
