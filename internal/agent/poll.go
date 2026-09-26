package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/internal/tunnel"
	"golang.zx2c4.com/wireguard/meshcfg"
)

func BootstrapPath() string {
	return filepath.Join(meshcfg.ConfDir(), meshcfg.AgentFileName)
}

func LoadBootstrap() (*meshcfg.AgentBootstrap, error) {
	path := BootstrapPath()
	var b meshcfg.AgentBootstrap
	if err := meshcfg.LoadJSON(path, &b); err != nil {
		return nil, err
	}
	if b.MasterURL == "" {
		return nil, fmt.Errorf("%s: masterUrl is required", path)
	}
	if b.APIKey() == "" {
		return nil, fmt.Errorf("%s: key is required (API secret from Master)", path)
	}
	// Persist normalized form (only masterUrl + key).
	norm := b.Normalized()
	if err := meshcfg.SaveJSON(path, norm, 0600); err != nil {
		return nil, err
	}
	b = norm
	if err := ensureEnrolled(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

func ensureEnrolled(b *meshcfg.AgentBootstrap) error {
	// Probe config; if unauthorized, treat key as enrollToken and enroll.
	client := &http.Client{Timeout: 15 * time.Second}
	url := stringsTrimSlash(b.MasterURL) + "/api/agent/config"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.Key)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach master: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return enrollWithKey(b)
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("agent config probe HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
}

func enrollWithKey(b *meshcfg.AgentBootstrap) error {
	hostname, _ := os.Hostname()
	payload, _ := json.Marshal(map[string]string{
		"enrollToken": b.Key,
		"name":        hostname,
		"role":        meshcfg.RoleClient,
	})
	url := stringsTrimSlash(b.MasterURL) + "/api/agent/enroll"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("enroll: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enroll HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	var out struct {
		NodeID    string `json:"nodeId"`
		NodeToken string `json:"nodeToken"`
		Address   string `json:"address"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("enroll json: %w", err)
	}
	if out.NodeToken == "" {
		return fmt.Errorf("enroll response missing nodeToken")
	}
	b.Key = out.NodeToken
	if err := meshcfg.SaveJSON(BootstrapPath(), b.Normalized(), 0600); err != nil {
		return fmt.Errorf("persist bootstrap: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: enrolled as node %s (%s)\n", out.NodeID, out.Address)
	return nil
}

// ConfigLoop polls Master and re-applies desired config when revision changes.
func ConfigLoop(
	dev *device.Device,
	logger *device.Logger,
	iface string,
	fwdPtr **tunnel.PortForwardManager,
	fwdMu *sync.Mutex,
	boot *meshcfg.AgentBootstrap,
	stop <-chan struct{},
) {
	appliedRev := -1
	client := &http.Client{Timeout: 30 * time.Second}
	url := stringsTrimSlash(boot.MasterURL) + "/api/agent/config"
	pollEvery := 10 * time.Second
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	fetch := func() {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			logger.Errorf("agent: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+boot.Key)
		if appliedRev >= 0 {
			req.Header.Set("If-None-Match", fmt.Sprintf(`"%d"`, appliedRev))
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: agent poll error: %v\n", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotModified {
			return
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: agent read: %v\n", err)
			return
		}
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "wireguard-go: agent config HTTP %d: %s\n", resp.StatusCode, truncateStr(string(body), 200))
			return
		}
		var desired meshcfg.DesiredConfig
		if err := json.Unmarshal(body, &desired); err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: agent config json: %v\n", err)
			return
		}
		if desired.Revision == appliedRev {
			return
		}

		if len(desired.Transport) > 0 {
			conn.SetTransportConfigJSON(desired.Transport)
		}
		applyIface := iface
		if desired.InterfaceName != "" {
			applyIface = desired.InterfaceName
		}
		if desired.PollInterval != "" {
			if d, err := time.ParseDuration(desired.PollInterval); err == nil && d >= time.Second {
				if d != pollEvery {
					pollEvery = d
					ticker.Reset(pollEvery)
				}
			}
		}

		newFwd := tunnel.NewPortForwardManager(logger)
		result, err := tunnel.ApplyDesiredConfig(dev, logger, applyIface, &desired)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: apply desired rev=%d: %v\n", desired.Revision, err)
			logger.Errorf("apply desired: %v", err)
			return
		}
		if err := newFwd.StartFromPeers(result.Peers); err != nil {
			newFwd.Close()
			fmt.Fprintf(os.Stderr, "wireguard-go: port forward: %v\n", err)
			logger.Errorf("port forward: %v", err)
			return
		}

		fwdMu.Lock()
		old := *fwdPtr
		*fwdPtr = newFwd
		fwdMu.Unlock()
		if old != nil {
			old.Close()
		}

		appliedRev = desired.Revision
		fmt.Fprintf(os.Stderr, "wireguard-go: applied mesh revision %d (node %s, %s, %d peers, %d forwards)\n",
			appliedRev, desired.NodeID, desired.Role, len(desired.Peers), len(desired.Forwards))
	}

	fetch()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			fetch()
		}
	}
}

func stringsTrimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
