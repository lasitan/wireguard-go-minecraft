package wire_test

import (
	. "golang.zx2c4.com/wireguard/src/core/wire"
	"net/netip"
	"reflect"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	b := EncodeFrame(TypePing, 42, []byte{1, 2, 3})
	f, err := DecodeFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != TypePing || f.Seq != 42 || !reflect.DeepEqual(f.Payload, []byte{1, 2, 3}) {
		t.Fatalf("frame mismatch: %+v", f)
	}
	if _, err := DecodeFrame([]byte{1, 2}); err == nil {
		t.Fatal("expected short error")
	}
}

func TestHelloRoundTrip(t *testing.T) {
	in := Hello{
		Token:    "tok",
		Version:  "1.2.3",
		PublicV4: netip.MustParseAddr("203.0.113.9"),
		PublicV6: netip.MustParseAddr("2001:db8::1"),
	}
	var out Hello
	if err := out.Unmarshal(in.Marshal()); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}

	empty := Hello{Token: "x"}
	var out2 Hello
	if err := out2.Unmarshal(empty.Marshal()); err != nil {
		t.Fatal(err)
	}
	if out2.PublicV4.IsValid() || out2.PublicV6.IsValid() {
		t.Fatalf("expected invalid addrs: %+v", out2)
	}
}

func TestStatsRoundTrip(t *testing.T) {
	var pk [32]byte
	pk[0], pk[31] = 7, 9
	in := Stats{
		UnixMilli: 1700000000000,
		RxBytes:   100,
		TxBytes:   200,
		Peers:     []PeerStat{{PublicKey: pk, RxBytes: 1, TxBytes: 2, LastHandshakeNano: 3, HandshakeRTTMicros: 4200}},
		IPs: []IPStat{
			{IP: netip.MustParseAddr("10.10.0.2"), RxBytes: 5, TxBytes: 6},
			{IP: netip.MustParseAddr("fd00::2"), RxBytes: 7, TxBytes: 8},
		},
		Forwards: []ForwardStat{{Protocol: "tcp", Listen: "3389", RxBytes: 9, TxBytes: 10}},
	}
	var out Stats
	if err := out.Unmarshal(in.Marshal()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestStatsWithoutRTTSection(t *testing.T) {
	var pk [32]byte
	in := Stats{UnixMilli: 1, Peers: []PeerStat{{PublicKey: pk, RxBytes: 1, HandshakeRTTMicros: 9}}}
	b := in.Marshal()
	var out Stats
	if err := out.Unmarshal(b[:len(b)-2-4]); err != nil {
		t.Fatalf("legacy agent payload must parse: %v", err)
	}
	if len(out.Peers) != 1 || out.Peers[0].HandshakeRTTMicros != 0 {
		t.Fatalf("got %+v", out)
	}
}

func TestStatsTruncated(t *testing.T) {
	b := Stats{UnixMilli: 1, IPs: []IPStat{{IP: netip.MustParseAddr("10.0.0.1")}}}.Marshal()
	var out Stats
	if err := out.Unmarshal(b[:len(b)-3]); err == nil {
		t.Fatal("expected error on truncated stats")
	}
}

func TestConfigAckAndError(t *testing.T) {
	in := ConfigAck{Revision: 9, OK: false, Error: "boom"}
	var out ConfigAck
	if err := out.Unmarshal(in.Marshal()); err != nil || out != in {
		t.Fatalf("ack: %+v %v", out, err)
	}
	e := ErrorMsg{Code: ErrCodeAuth, Message: "bad token"}
	var eo ErrorMsg
	if err := eo.Unmarshal(e.Marshal()); err != nil || eo != e {
		t.Fatalf("err: %+v %v", eo, err)
	}
	h := HelloAck{NodeID: "abc", StatsIntervalMs: 2000}
	var ho HelloAck
	if err := ho.Unmarshal(h.Marshal()); err != nil || ho != h {
		t.Fatalf("helloack: %+v %v", ho, err)
	}
}
