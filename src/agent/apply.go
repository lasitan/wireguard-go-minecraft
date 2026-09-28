package agent

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/core"
	tunconf "golang.zx2c4.com/wireguard/src/tunnel/config"
	"golang.zx2c4.com/wireguard/src/tunnel/portfwd"
	"golang.zx2c4.com/wireguard/src/wireguard/conn"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

// applier applies DesiredConfig revisions; shared by the WS and HTTP paths.
type applier struct {
	dev    *device.Device
	logger *device.Logger
	iface  string
	fwdPtr **portfwd.PortForwardManager
	fwdMu  *sync.Mutex

	mu         sync.Mutex
	appliedRev int
	applied    *core.DesiredConfig // last applied revision; basis for hot diffs
	pollEvery  time.Duration
}

func newApplier(dev *device.Device, logger *device.Logger, iface string, fwdPtr **portfwd.PortForwardManager, fwdMu *sync.Mutex) *applier {
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
func (a *applier) apply(desired *core.DesiredConfig) error {
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

	result, err := tunconf.ApplyDesiredConfig(a.dev, a.logger, applyIface, desired, a.applied)
	if err != nil {
		a.logger.Errorf("apply desired: %v", err)
		return fmt.Errorf("apply desired rev=%d: %w", desired.Revision, err)
	}
	a.applied = desired
	a.appliedRev = desired.Revision

	a.fwdMu.Lock()
	if *a.fwdPtr == nil {
		*a.fwdPtr = portfwd.NewPortForwardManager(a.logger)
	}
	fwd := *a.fwdPtr
	a.fwdMu.Unlock()
	if err := fwd.Reconcile(result.Peers); err != nil {
		a.logger.Errorf("port forward: %v", err)
		fmt.Fprintf(os.Stderr, "wireguard-go: warning: port forward: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "wireguard-go: applied mesh revision %d (node %s, %s, %d peers, %d forwards)\n",
		a.appliedRev, desired.NodeID, desired.Role, len(desired.Peers), len(desired.Forwards))
	return nil
}
