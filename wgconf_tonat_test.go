/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

package main

import "testing"

func TestParseToNATEndpoint(t *testing.T) {
	ep, err := parseToNATEndpoint("10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if ep != "10.0.0.2:25565" {
		t.Fatalf("got %q", ep)
	}

	ep, err = parseToNATEndpoint("10.0.0.2:1234")
	if err != nil {
		t.Fatal(err)
	}
	if ep != "10.0.0.2:1234" {
		t.Fatalf("got %q", ep)
	}

	ep, err = parseToNATEndpoint("gateway.local")
	if err != nil {
		t.Fatal(err)
	}
	if ep != "gateway.local:25565" {
		t.Fatalf("got %q", ep)
	}
}
