package api

import (
	"fmt"
	"net/http"
	"strings"

	"golang.zx2c4.com/wireguard/src/update"
)

type agentInstallCommands struct {
	MasterURL string `json:"masterUrl"`
	Linux     string `json:"linux"`
	LinuxCN   string `json:"linuxCn"`
	Windows   string `json:"windows"`
}

func masterPublicURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); p != "" {
		scheme = p
	}
	host := r.Host
	if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); h != "" {
		host = h
	}
	return strings.TrimSuffix(scheme+"://"+host, "/")
}

func buildAgentInstallCommands(masterURL, enrollKey, role, endpoint string) agentInstallCommands {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "client"
	}
	envLinux := fmt.Sprintf(
		"LASITAN_BOOTSTRAP=agent LASITAN_MASTER_URL=%s LASITAN_ENROLL_KEY=%s LASITAN_ROLE=%s",
		shellQuoteBash(masterURL),
		shellQuoteBash(enrollKey),
		shellQuoteBash(role),
	)
	if role == "server" {
		if ep := strings.TrimSpace(endpoint); ep != "" {
			envLinux += " LASITAN_ENDPOINT=" + shellQuoteBash(ep)
		}
		envLinux += " LASITAN_LISTEN_PORT=25590"
	}
	linux := fmt.Sprintf("curl -fsSL %s | sudo env %s bash", update.InstallScriptURL, envLinux)
	linuxCN := fmt.Sprintf(
		"curl -fsSL https://ghfast.top/%s | sudo env %s LASITAN_GH_PROXY=https://ghfast.top/ bash",
		update.InstallScriptURL, envLinux,
	)

	winEnv := buildWindowsEnv(masterURL, enrollKey, role, endpoint)
	windows := fmt.Sprintf(`powershell -ExecutionPolicy Bypass -c "%s; irm %s | iex"`, winEnv, update.InstallPS1URL)

	return agentInstallCommands{MasterURL: masterURL, Linux: linux, LinuxCN: linuxCN, Windows: windows}
}

func buildWindowsEnv(masterURL, enrollKey, role, endpoint string) string {
	parts := []string{
		"$env:LASITAN_BOOTSTRAP='agent'",
		"$env:LASITAN_MASTER_URL=" + shellQuotePS(masterURL),
		"$env:LASITAN_ENROLL_KEY=" + shellQuotePS(enrollKey),
		"$env:LASITAN_ROLE=" + shellQuotePS(role),
	}
	if role == "server" {
		if ep := strings.TrimSpace(endpoint); ep != "" {
			parts = append(parts, "$env:LASITAN_ENDPOINT="+shellQuotePS(ep))
		}
		parts = append(parts, "$env:LASITAN_LISTEN_PORT='25590'")
	}
	return strings.Join(parts, "; ")
}

func shellQuoteBash(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellQuotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
