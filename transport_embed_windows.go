/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed wireguard-go-transport.json.example
var embeddedTransportConfigExample []byte

// ensureTransportConfigWindows writes the embedded transport example to
// %ProgramData%\wireguard\wireguard-go-transport.json when missing so deep MC
// camouflage secrets match a fresh Debian install (same example file).
func ensureTransportConfigWindows() error {
	if len(embeddedTransportConfigExample) == 0 {
		return fmt.Errorf("embedded transport config example is empty")
	}
	dir := wgConfDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	dest := filepath.Join(dir, "wireguard-go-transport.json")
	if _, err := os.Stat(dest); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(dir, "wireguard-go-transport-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp transport config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(embeddedTransportConfigExample); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("install transport config to %s: %w", dest, err)
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: released %s — edit loginPluginSecret to match the peer\n", dest)
	return nil
}
