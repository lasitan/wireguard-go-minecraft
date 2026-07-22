//go:build !windows && !wasm

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

func applyIfaceNetConfig(iface string, cfg ifaceNetConfig, logger *device.Logger) error {
	const ipTimeout = 5 * time.Second
	runIP := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), ipTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("ip %v timed out after %s", args, ipTimeout)
			}
			return fmt.Errorf("ip %v: %w (%s)", args, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	if cfg.mtu > 0 {
		if err := runIP("link", "set", "dev", iface, "mtu", strconv.Itoa(cfg.mtu)); err != nil {
			return fmt.Errorf("set mtu %d on %s: %w", cfg.mtu, iface, err)
		}
		logger.Verbosef("Set %s mtu %d", iface, cfg.mtu)
	}
	if err := runIP("link", "set", "dev", iface, "up"); err != nil {
		return fmt.Errorf("set %s up: %w", iface, err)
	}
	for _, addr := range cfg.addresses {
		if err := runIP("addr", "replace", addr, "dev", iface); err != nil {
			return fmt.Errorf("addr %s on %s: %w", addr, iface, err)
		}
		logger.Verbosef("Assigned %s to %s", addr, iface)
		fmt.Fprintf(os.Stderr, "wireguard-go: assigned %s to %s\n", addr, iface)
	}
	return nil
}
