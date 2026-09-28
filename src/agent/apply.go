package agent

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
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
	masterHost string // where "@master:PORT" relay endpoints point
}

func newApplier(dev *device.Device, logger *device.Logger, iface string, fwdPtr **portfwd.PortForwardManager, fwdMu *sync.Mutex, masterURL string) *applier {
	host := ""
	if u, err := url.Parse(masterURL); err == nil {
		host = u.Hostname()
	}
	return &applier{dev: dev, logger: logger, iface: iface, fwdPtr: fwdPtr, fwdMu: fwdMu, appliedRev: -1, pollEvery: 10 * time.Second, masterHost: host}
}

// resolveRelay points Master-relay peers at the host this agent reaches Master on.
func (a *applier) resolveRelay(d *core.DesiredConfig) *core.DesiredConfig {
	var out *core.DesiredConfig
	for i, p := range d.Peers {
		port, ok := strings.CutPrefix(p.Endpoint, core.RelayEndpointHost+":")
		if !ok {
			continue
		}
		if out == nil {
			cp := *d
			cp.Peers = append([]core.DesiredPeer(nil), d.Peers...)
			out = &cp
		}
		if a.masterHost == "" {
			out.Peers[i].Endpoint = ""
			continue
		}
		out.Peers[i].Endpoint = net.JoinHostPort(a.masterHost, port)
	}
	if out == nil {
		return d
	}
	return out
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
	desired = a.resolveRelay(desired)
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
		fmt.Fprintf(os.Stderr, "lasitan-cluster: warning: port forward: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "lasitan-cluster: applied mesh revision %d (node %s, %s, %d peers, %d forwards)\n",
		a.appliedRev, desired.NodeID, desired.Role, len(desired.Peers), len(desired.Forwards))
	return nil
}
