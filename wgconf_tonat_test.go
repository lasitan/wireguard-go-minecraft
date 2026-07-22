/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import "testing"

func TestBuildNatGatewayToNATRequiresEndpoint(t *testing.T) {
	peers := []peerHookConfig{{
		publicKeyB64:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		publicKeyHex:  "0000000000000000000000000000000000000000000000000000000000000000",
		label:         "peer",
		allowedHosts:  []string{"0.0.0.0/0"},
		endpoint:      "",
	}}
	_, _, err := buildNatGatewayResult(true, "", 0, ifaceNetConfig{}, peers)
	if err == nil {
		t.Fatal("expected error when ToNAT=true and Peer has no Endpoint")
	}

	peers[0].endpoint = "10.0.0.2:25565"
	nat, _, err := buildNatGatewayResult(true, "", 0, ifaceNetConfig{}, peers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nat.toNATClient {
		t.Fatal("expected toNATClient")
	}
}
