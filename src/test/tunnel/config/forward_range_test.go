package config_test

import (
	"golang.zx2c4.com/wireguard/src/core"
	. "golang.zx2c4.com/wireguard/src/tunnel/config"
	"testing"
)

func TestExpandForwardRange(t *testing.T) {
	got, err := ExpandForward(core.DesiredForward{
		Protocol: "tcp",
		Listen:   "1000-1002",
		DestHost: "10.10.0.2",
		DestPort: 2000,
	})
	if err != nil || len(got) != 3 {
		t.Fatalf("expand: %d %v", len(got), err)
	}
	if got[0].ListenPort != 1000 || got[0].DestPort != 2000 || got[2].ListenPort != 1002 || got[2].DestPort != 2002 {
		t.Fatalf("mapping: %+v", got)
	}
	if got[0].StatListen != "1000-1002" || got[0].BestEffort {
		t.Fatalf("label: %+v", got[0])
	}

	star, err := ExpandForward(core.DesiredForward{
		Protocol: "udp",
		Listen:   "*",
		DestHost: "10.10.0.2",
		Dest:     "*",
	})
	if err != nil || len(star) != 65535 || star[0].ListenPort != 1 || star[0].DestPort != 1 || star[65534].DestPort != 65535 || !star[0].BestEffort {
		t.Fatalf("star: n=%d err=%v first=%+v", len(star), err, star[0])
	}
}
