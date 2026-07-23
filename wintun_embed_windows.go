/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Official signed Wintun 0.14.1 (amd64) from https://www.wintun.net/
// Redistribution permitted under the license shipped in third_party/wintun/.
//
//go:embed third_party/wintun/amd64/wintun.dll
var embeddedWintunDLL []byte

// ensureWintunDLL writes the embedded wintun.dll next to this executable when
// missing or outdated. golang.zx2c4.com/wintun loads via
// LOAD_LIBRARY_SEARCH_APPLICATION_DIR, so the DLL must live beside the .exe.
func ensureWintunDLL() error {
	if len(embeddedWintunDLL) == 0 {
		return fmt.Errorf("embedded wintun.dll is empty")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dest := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if existing, err := os.ReadFile(dest); err == nil && bytes.Equal(existing, embeddedWintunDLL) {
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), "wintun-*.dll.tmp")
	if err != nil {
		return fmt.Errorf("create temp for wintun.dll beside %s: %w (need write access next to the exe)", exe, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(embeddedWintunDLL); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write wintun.dll: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		// Windows may refuse rename over a loaded DLL; try replace via remove+rename.
		_ = os.Remove(dest)
		if err2 := os.Rename(tmpName, dest); err2 != nil {
			return fmt.Errorf("install wintun.dll to %s: %w", dest, err2)
		}
	}
	fmt.Fprintf(os.Stderr, "wireguard-go: released embedded wintun.dll → %s\n", dest)
	return nil
}
