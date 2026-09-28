//go:build !windows && !wasm

package config

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

// syncIfaceRoutes adds/removes routes through the tunnel interface.
func syncIfaceRoutes(iface string, add, del []string, logger *device.Logger) error {
	run := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("ip %v: %w (%s)", args, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	for _, p := range del {
		_ = run("route", "del", p, "dev", iface)
	}
	var firstErr error
	for _, p := range add {
		if err := run("route", "replace", p, "dev", iface); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	logger.Verbosef("Routes on %s: +%v -%v", iface, add, del)
	return firstErr
}
