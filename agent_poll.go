package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/meshcfg"
)

func agentBootstrapPath() string {
	return filepath.Join(wgConfDir(), meshcfg.AgentFileName)
}

func loadAgentBootstrap() (*meshcfg.AgentBootstrap, error) {
	path := agentBootstrapPath()
	var b meshcfg.AgentBootstrap
	if err := meshcfg.LoadJSON(path, &b); err != nil {
		return nil, err
	}
	if b.MasterURL == "" || b.NodeID == "" || b.NodeToken == "" {
		return nil, fmt.Errorf("%s: masterUrl, nodeId, nodeToken are required", path)
	}
	return &b, nil
}

// agentConfigLoop polls Master and re-applies desired config when revision changes.
func agentConfigLoop(
	dev *device.Device,
	logger *device.Logger,
	iface string,
	fwdPtr **portForwardManager,
	fwdMu *sync.Mutex,
	boot *meshcfg.AgentBootstrap,
	stop <-chan struct{},
) {
	appliedRev := -1
	client := &http.Client{Timeout: 30 * time.Second}
	url := stringsTrimSlash(boot.MasterURL) + "/api/agent/config"

	fetch := func() {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			logger.Errorf("agent: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+boot.NodeToken)
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

		newFwd := newPortForwardManager(logger)
		result, err := applyDesiredConfig(dev, logger, iface, &desired)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wireguard-go: apply desired rev=%d: %v\n", desired.Revision, err)
			logger.Errorf("apply desired: %v", err)
			return
		}
		if err := newFwd.StartFromPeers(result.peers); err != nil {
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
		fmt.Fprintf(os.Stderr, "wireguard-go: applied mesh revision %d (%s, %d peers, %d forwards)\n",
			appliedRev, desired.Role, len(desired.Peers), len(desired.Forwards))
	}

	fetch()
	ticker := time.NewTicker(boot.PollDuration())
	defer ticker.Stop()
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
