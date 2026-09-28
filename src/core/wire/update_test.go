package wire

import (
	"reflect"
	"testing"
)

func TestUpdateCmdRoundTrip(t *testing.T) {
	for _, in := range []UpdateCmd{
		{},
		{Force: true},
		{Proxy: "https://ghfast.top/"},
		{Force: true, Proxy: "https://ghfast.top/", Release: []byte(`{"tag":"v1"}`)},
	} {
		var out UpdateCmd
		if err := out.Unmarshal(in.Marshal()); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("got %+v, want %+v", out, in)
		}
	}
}

func TestUpdateCmdLegacyPayload(t *testing.T) {
	if b := (UpdateCmd{Force: true}).Marshal(); len(b) != 1 {
		t.Fatalf("plain command should stay one byte, got %d", len(b))
	}
	var out UpdateCmd
	if err := out.Unmarshal([]byte{1}); err != nil || !out.Force || out.Proxy != "" {
		t.Fatalf("legacy decode: %+v %v", out, err)
	}
}
