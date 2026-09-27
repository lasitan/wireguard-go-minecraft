package agent

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/internal/tunnel"
	"golang.zx2c4.com/wireguard/meshcfg"
)

// applier applies DesiredConfig revisions; shared by the WS and HTTP paths.
type applier struct {
	dev    *device.Device
	logger *device.Logger
	iface  string
	fwdPtr **tunnel.PortForwardManager
	fwdMu  *sync.Mutex

	mu         sync.Mutex
	appliedRev int
	pollEvery  time.Duration
}

func newApplier(dev *device.Device, logger *device.Logger, iface string, fwdPtr **tunnel.PortForwardManager, fwdMu *sync.Mutex) *applier {
	return &applier{dev: dev, logger: logger, iface: iface, fwdPtr: fwdPtr, fwdMu: fwdMu, appliedRev: -1, pollEvery: 10 * time.Second}
}

func (a *applier) revision() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.appliedRev
}

func (a *applier) pollInterval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pollEvery
}

// apply is a no-op for an already applied revision.
func (a *applier) apply(desired *meshcfg.DesiredConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if desired.Revision == a.appliedRev {
		return nil
	}
	if len(desired.Transport) > 0 {
		conn.SetTransportConfigJSON(desired.Transport)
	}
	applyIface := a.iface
	if desired.InterfaceName != "" {
		applyIface = desired.InterfaceName
	}
	if desired.PollInterval != "" {
		if d, err := time.ParseDuration(desired.PollInterval); err == nil && d >= time.Second {
			a.pollEvery = d
		}
	}

	newFwd := tunnel.NewPortForwardManager(a.logger)
	result, err := tunnel.ApplyDesiredConfig(a.dev, a.logger, applyIface, desired)
	if err != nil {
		a.logger.Errorf("apply desired: %v", err)
		return fmt.Errorf("apply desired rev=%d: %w", desired.Revision, err)
	}
	if err := newFwd.StartFromPeers(result.Peers); err != nil {
		newFwd.Close()
		a.logger.Errorf("port forward: %v", err)
		return fmt.Errorf("port forward: %w", err)
	}

	a.fwdMu.Lock()
	old := *a.fwdPtr
	*a.fwdPtr = newFwd
	a.fwdMu.Unlock()
	if old != nil {
		old.Close()
	}

	a.appliedRev = desired.Revision
	fmt.Fprintf(os.Stderr, "wireguard-go: applied mesh revision %d (node %s, %s, %d peers, %d forwards)\n",
		a.appliedRev, desired.NodeID, desired.Role, len(desired.Peers), len(desired.Forwards))
	return nil
}
