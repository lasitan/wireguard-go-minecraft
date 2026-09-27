//go:build windows

package config

import "golang.zx2c4.com/wireguard/src/wireguard/device"

func EnableIPForward(logger *device.Logger) error {
	// Windows IP forwarding is handled by nat gateway path when needed;
	// for Master-desired server mode we log and continue.
	logger.Verbosef("ip forward: skipped on Windows (use OS forwarding if required)")
	return nil
}
