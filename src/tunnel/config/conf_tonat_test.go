/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package config

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/tunnel/spec"
)

func TestBuildNatGatewayToNATRequiresEndpoint(t *testing.T) {
	peers := []spec.PeerHookConfig{{
		PublicKeyB64: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		PublicKeyHex: "0000000000000000000000000000000000000000000000000000000000000000",
		Label:        "peer",
		AllowedHosts: []string{"0.0.0.0/0"},
		Endpoint:     "",
	}}
	_, _, err := buildNatGatewayResult(true, "", 0, IfaceNetConfig{}, peers)
	if err == nil {
		t.Fatal("expected error when ToNAT=true and Peer has no Endpoint")
	}

	peers[0].Endpoint = "10.0.0.2:25565"
	nat, _, err := buildNatGatewayResult(true, "", 0, IfaceNetConfig{}, peers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nat.ToNATClient {
		t.Fatal("expected toNATClient")
	}
}
