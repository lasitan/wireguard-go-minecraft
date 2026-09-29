package wire_test

import (
	"reflect"
	"testing"

	. "golang.zx2c4.com/wireguard/src/core/wire"
)

func TestUIMessagesRoundTrip(t *testing.T) {
	var h UIHello
	if err := h.Unmarshal(UIHello{Token: "tok"}.Marshal()); err != nil || h.Token != "tok" {
		t.Fatalf("hello: %+v %v", h, err)
	}
	var a UIHelloAck
	if err := a.Unmarshal(UIHelloAck{PresenceIntervalMs: 2000}.Marshal()); err != nil || a.PresenceIntervalMs != 2000 {
		t.Fatalf("ack: %+v %v", a, err)
	}
	var s UISubscribe
	if err := s.Unmarshal(UISubscribe{NodeID: "n1"}.Marshal()); err != nil || s.NodeID != "n1" {
		t.Fatalf("subscribe: %+v %v", s, err)
	}

	in := Presence{NowMs: 1_700_000_000_123, Nodes: []PresenceNode{
		{NodeID: "a", Link: LinkWS, LastSeenMs: 1_700_000_000_000, RxRate: 1234.5, TxRate: 0.25, RTTMicros: 3500},
		{NodeID: "b", Link: LinkOffline},
	}}
	var out Presence
	if err := out.Unmarshal(in.Marshal()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestPresenceTruncated(t *testing.T) {
	b := Presence{Nodes: []PresenceNode{{NodeID: "a"}}}.Marshal()
	var out Presence
	if err := out.Unmarshal(b[:len(b)-1]); err == nil {
		t.Fatal("truncated presence decoded")
	}
}
