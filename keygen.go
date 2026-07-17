/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/curve25519"
)

func printUsage() {
	fmt.Printf(`Usage:
  %s [-f/--foreground] INTERFACE-NAME
  %s genkey
  %s pubkey
  %s genpsk
  %s --version

Key commands (same as wg(8)):
  genkey   Generate a private key on stdout (base64)
  pubkey   Read a private key from stdin; write public key to stdout
  genpsk   Generate a preshared key on stdout (base64)
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
}

// handleKeyCommand runs wg-compatible key utilities.
// Returns true if a key command was handled (caller should return).
func handleKeyCommand() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "genkey":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: wireguard-go genkey")
			os.Exit(ExitSetupFailed)
		}
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			fmt.Fprintf(os.Stderr, "genkey: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		clampKey(&key)
		fmt.Println(base64.StdEncoding.EncodeToString(key[:]))
		return true

	case "genpsk":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: wireguard-go genpsk")
			os.Exit(ExitSetupFailed)
		}
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			fmt.Fprintf(os.Stderr, "genpsk: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		// Preshared keys are not Curve25519 scalars; do not clamp.
		fmt.Println(base64.StdEncoding.EncodeToString(key[:]))
		return true

	case "pubkey":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: wireguard-go pubkey < privatekey")
			os.Exit(ExitSetupFailed)
		}
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pubkey: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		b64 := strings.TrimSpace(string(raw))
		priv, err := decodeKey32(b64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pubkey: %v\n", err)
			os.Exit(ExitSetupFailed)
		}
		clampKey(&priv)
		var pub [32]byte
		curve25519.ScalarBaseMult(&pub, &priv)
		fmt.Println(base64.StdEncoding.EncodeToString(pub[:]))
		return true

	default:
		return false
	}
}

func clampKey(key *[32]byte) {
	key[0] &= 248
	key[31] = (key[31] & 127) | 64
}

func decodeKey32(b64 string) ([32]byte, error) {
	var out [32]byte
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
	}
	if err != nil {
		return out, fmt.Errorf("invalid base64 key")
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	copy(out[:], raw)
	return out, nil
}
