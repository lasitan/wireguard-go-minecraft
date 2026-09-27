//go:build !windows

package config

import (
	"fmt"
	"os"
	"strings"

	"golang.zx2c4.com/wireguard/src/wireguard/device"
)

func EnableIPForward(logger *device.Logger) error {
	v4, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(v4)) != "1" {
		if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644); err != nil {
			return fmt.Errorf("ipv4 ip_forward: %w", err)
		}
		logger.Verbosef("enabled net.ipv4.ip_forward")
		fmt.Fprintln(os.Stderr, "wireguard-go: enabled net.ipv4.ip_forward")
	}
	return nil
}
