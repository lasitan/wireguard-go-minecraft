package core_test

import (
	. "golang.zx2c4.com/wireguard/src/core"
	"testing"
)

func TestParsePortSpan(t *testing.T) {
	all, err := ParsePortSpan("*")
	if err != nil || all.Start != 1 || all.End != 65535 || all.Canon() != "*" {
		t.Fatalf("* : %+v %v", all, err)
	}
	one, err := ParsePortSpan("80")
	if err != nil || one.Canon() != "80" {
		t.Fatalf("80: %+v %v", one, err)
	}
	rng, err := ParsePortSpan("1000-1002")
	if err != nil || rng.Len() != 3 || rng.Canon() != "1000-1002" {
		t.Fatalf("range: %+v %v", rng, err)
	}
	if _, err := ParsePortSpan("1002-1000"); err == nil {
		t.Fatal("reversed range accepted")
	}
	if _, err := ParsePortSpan("0-10"); err == nil {
		t.Fatal("port 0 accepted")
	}
}

func TestResolveDestMapping(t *testing.T) {
	listen, err := ParsePortSpan("1000-1002")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDest(listen, "2000", 0)
	if err != nil || got.Canon() != "2000-2002" {
		t.Fatalf("offset: %+v %v", got, err)
	}
	same, err := ResolveDest(listen, "*", 0)
	if err != nil || same != listen {
		t.Fatalf("identity: %+v %v", same, err)
	}
	all, _ := ParsePortSpan("*")
	full, err := ResolveDest(all, "*", 0)
	if err != nil || full.Canon() != "*" {
		t.Fatalf("all: %+v %v", full, err)
	}
	if _, err := ResolveDest(listen, "80", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDest(listen, "65534", 0); err == nil {
		t.Fatal("dest run past 65535 accepted")
	}
	host, span, err := ParseListenExpr("1.2.3.4:*")
	if err != nil || host != "1.2.3.4" || span.Canon() != "*" {
		t.Fatalf("ip star: %s %+v %v", host, span, err)
	}
}
