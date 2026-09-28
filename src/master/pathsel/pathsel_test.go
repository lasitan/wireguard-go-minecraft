package pathsel

import (
	"testing"

	"golang.zx2c4.com/wireguard/src/core"
)

func TestChoose(t *testing.T) {
	cands := map[string]map[string][]string{
		"c": {"10.20.0.0/24": {"b1", "b2"}, "10.10.0.0/24": {"a1"}},
	}
	rtts := map[[2]string]float64{}
	rtt := func(a, b string) (float64, bool) {
		r, ok := rtts[[2]string{a, b}]
		return r, ok
	}
	dead := map[string]bool{}
	alive := func(id string) bool { return !dead[id] }
	pick := func(cur core.PathChoices) string {
		return Choose(cur, cands, rtt, alive)["c"]["10.20.0.0/24"]
	}

	if got := pick(nil); got != "b1" {
		t.Fatalf("no data: first candidate, got %s", got)
	}
	if got := Choose(nil, cands, rtt, alive)["c"]["10.10.0.0/24"]; got != "a1" {
		t.Fatalf("single-candidate group: %q", got)
	}

	rtts[[2]string{"c", "b1"}] = 40
	rtts[[2]string{"c", "b2"}] = 12
	if got := pick(nil); got != "b2" {
		t.Fatalf("fastest wins, got %s", got)
	}
	cur := core.PathChoices{"c": {"10.20.0.0/24": "b1"}}
	if got := pick(cur); got != "b2" {
		t.Fatalf("clearly faster switches, got %s", got)
	}

	rtts[[2]string{"c", "b2"}] = 38
	if got := pick(cur); got != "b1" {
		t.Fatalf("hysteresis keeps current, got %s", got)
	}

	dead["b1"] = true
	if got := pick(cur); got != "b2" {
		t.Fatalf("dead gateway is replaced, got %s", got)
	}

	delete(rtts, [2]string{"c", "b2"})
	rtts[[2]string{"b2", "c"}] = 5
	dead["b1"] = false
	if got := pick(nil); got != "b2" {
		t.Fatalf("reverse measurement counts, got %s", got)
	}
}

func TestRelayIsLastResort(t *testing.T) {
	r := core.RelayNodeID
	cands := map[string]map[string][]string{"c": {"s": {"b1", r}}, "d": {"s": {r}}}
	dead := map[string]bool{}
	alive := func(id string) bool { return !dead[id] }
	none := func(a, b string) (float64, bool) { return 0, false }

	if got := Choose(nil, cands, none, alive); got["c"]["s"] != "b1" || got["d"]["s"] != r {
		t.Fatalf("mother without RTT still beats the relay: %+v", got)
	}
	dead["b1"] = true
	cur := Choose(nil, cands, none, alive)
	if cur["c"]["s"] != r {
		t.Fatalf("all mothers dead: relay, got %+v", cur)
	}
	dead["b1"] = false
	if got := Choose(cur, cands, none, alive); got["c"]["s"] != "b1" {
		t.Fatalf("mother back: leave the relay, got %+v", got)
	}
}
