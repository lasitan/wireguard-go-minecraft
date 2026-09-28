package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/tunnel/ipcounter"
	"golang.zx2c4.com/wireguard/src/tunnel/portfwd"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

func BootstrapPath() string {
	return filepath.Join(config.ConfDir(), config.AgentFileName)
}

// LoadBootstrap reads the local agent bootstrap file. It does not contact Master;
// enroll and config sync happen in ConfigLoop so a slow network at boot does not
// leave the tunnel running with zero peers until a manual restart.
func LoadBootstrap() (*config.AgentBootstrap, error) {
	path := BootstrapPath()
	var b config.AgentBootstrap
	if err := config.LoadJSON(path, &b); err != nil {
		return nil, err
	}
	if b.MasterURL == "" {
		return nil, fmt.Errorf("%s: masterUrl is required", path)
	}
	if b.APIKey() == "" {
		return nil, fmt.Errorf("%s: key is required (API secret from Master)", path)
	}
	norm := b.Normalized()
	if err := config.SaveJSON(path, norm, 0600); err != nil {
		return nil, err
	}
	return &norm, nil
}

func ensureEnrolled(b *config.AgentBootstrap) error {
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

func enrollWithKey(b *config.AgentBootstrap) error {
	hostname, _ := os.Hostname()
	role := strings.ToLower(strings.TrimSpace(b.Role))
	if role == "" {
		role = core.RoleClient
	}
	payload, _ := json.Marshal(map[string]any{
		"enrollToken": b.Key,
		"name":        hostname,
		"role":        role,
		"endpoint":    strings.TrimSpace(b.Endpoint),
		"listenPort":  b.ListenPort,
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
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("enroll json: %w", err)
	}
	if out.NodeToken == "" {
		return fmt.Errorf("enroll response missing nodeToken")
	}
	b.Key = out.NodeToken
	if err := config.SaveJSON(BootstrapPath(), b.Normalized(), 0600); err != nil {
		return fmt.Errorf("persist bootstrap: %w", err)
	}
	fmt.Fprintf(os.Stderr, "lasitan-cluster: enrolled as %s node %s (%s)\n", out.Role, out.NodeID, out.Address)
	return nil
}

const (
	wsBackoffMin = time.Second
	wsBackoffMax = time.Minute
	// httpFallbackWindow is how long to poll over HTTP before retrying WS
	// (e.g. an older Master without /api/agent/ws).
	httpFallbackWindow = time.Minute
)

// ConfigLoop keeps the agent in sync with Master. It prefers the binary
// WebSocket long connection (config push + stats) and falls back to HTTP
// polling while the WS endpoint is unreachable.
func ConfigLoop(
	dev *device.Device,
	logger *device.Logger,
	iface string,
	fwdPtr **portfwd.PortForwardManager,
	fwdMu *sync.Mutex,
	boot *config.AgentBootstrap,
	stop <-chan struct{},
) {
	ap := newApplier(dev, logger, iface, fwdPtr, fwdMu, boot.MasterURL)
	counter := ipcounter.NewIPCounter()
	dev.SetTrafficCounter(counter)
	defer dev.SetTrafficCounter(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stop
		cancel()
	}()

	ws := &wsClient{boot: boot, dev: dev, counter: counter, ap: ap}
	backoff := wsBackoffMin
	for ctx.Err() == nil {
		if err := ensureEnrolled(boot); err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: waiting for Master (%v)\n", err)
			if !sleepCtx(ctx, jitter(backoff)) {
				return
			}
			if backoff < wsBackoffMax {
				backoff *= 2
				if backoff > wsBackoffMax {
					backoff = wsBackoffMax
				}
			}
			continue
		}
		backoff = wsBackoffMin
		connected, err := ws.run(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = wsBackoffMin
			fmt.Fprintf(os.Stderr, "lasitan-cluster: master link lost: %v; reconnecting\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: master ws unavailable (%v); HTTP polling for %s\n", err, httpFallbackWindow)
			pollHTTP(ctx, boot, ap, logger, httpFallbackWindow)
		}
		if !sleepCtx(ctx, jitter(backoff)) {
			return
		}
		backoff *= 2
		if backoff > wsBackoffMax {
			backoff = wsBackoffMax
		}
	}
}

func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d)))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// pollHTTP polls /api/agent/config with ETag for up to window.
func pollHTTP(ctx context.Context, boot *config.AgentBootstrap, ap *applier, logger *device.Logger, window time.Duration) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := stringsTrimSlash(boot.MasterURL) + "/api/agent/config"
	deadline := time.Now().Add(window)

	fetch := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			logger.Errorf("agent: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+boot.Key)
		if rev := ap.revision(); rev >= 0 {
			req.Header.Set("If-None-Match", fmt.Sprintf(`"%d"`, rev))
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: agent poll error: %v\n", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotModified {
			return
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: agent read: %v\n", err)
			return
		}
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: agent config HTTP %d: %s\n", resp.StatusCode, truncateStr(string(body), 200))
			return
		}
		var desired core.DesiredConfig
		if err := json.Unmarshal(body, &desired); err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: agent config json: %v\n", err)
			return
		}
		if err := ap.apply(&desired); err != nil {
			fmt.Fprintf(os.Stderr, "lasitan-cluster: %v\n", err)
		}
	}

	fetch()
	for time.Now().Before(deadline) {
		if !sleepCtx(ctx, ap.pollInterval()) {
			return
		}
		fetch()
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
