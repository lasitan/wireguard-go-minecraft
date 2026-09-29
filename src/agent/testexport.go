package agent

import (
	"context"
	"sync"

	"golang.zx2c4.com/wireguard/src/core"
	"golang.zx2c4.com/wireguard/src/core/config"
	"golang.zx2c4.com/wireguard/src/tunnel/portfwd"
	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

type Applier = applier

func NewApplier(dev *device.Device, logger *device.Logger, iface string, fwdPtr **portfwd.PortForwardManager, fwdMu *sync.Mutex, masterURL string) *Applier {
	return newApplier(dev, logger, iface, fwdPtr, fwdMu, masterURL)
}

func (a *Applier) ResolveRelay(d *core.DesiredConfig) *core.DesiredConfig {
	return a.resolveRelay(d)
}

func WsURL(masterURL string) string {
	return wsURL(masterURL)
}

type WsClient = wsClient

func NewWsClientForTest(boot *config.AgentBootstrap) *WsClient {
	return &wsClient{boot: boot}
}

func (c *WsClient) Run(ctx context.Context) (connected bool, err error) {
	return c.run(ctx)
}
