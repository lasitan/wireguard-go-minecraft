package stats

import (
	"net/netip"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/src/master/storetest"

	"golang.zx2c4.com/wireguard/src/core/wire"
)

func TestStatsIngestDeltaAndRestart(t *testing.T) {
	st := storetest.Open(t, "10.10.0.0/24")
	svc := NewStatsService(st)
	ip := netip.MustParseAddr("10.10.0.2")
	t0 := time.Unix(1_700_000_000, 0)

	svc.MarkConnected("n1", t0)
	svc.Ingest("n1", &wire.Stats{RxBytes: 1000, TxBytes: 500, IPs: []wire.IPStat{{IP: ip, RxBytes: 100, TxBytes: 50}}}, t0)
	svc.Ingest("n1", &wire.Stats{RxBytes: 3000, TxBytes: 900, IPs: []wire.IPStat{{IP: ip, RxBytes: 300, TxBytes: 90}}}, t0.Add(2*time.Second))
	// Agent restart: counters drop; new value counts as delta.
	svc.Ingest("n1", &wire.Stats{RxBytes: 200, TxBytes: 100}, t0.Add(4*time.Second))

	p := svc.takePending()
	d := p["n1"]
	if d == nil || d.Rx != 2000+200 || d.Tx != 400+100 {
		t.Fatalf("delta: %+v", d)
	}
	if v := d.IPs["10.10.0.2"]; v != [2]uint64{200, 40} {
		t.Fatalf("ip delta: %v", v)
	}

	if err := st.FlushTraffic(p, t0.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	tot, err := st.TrafficTotal("n1")
	if err != nil || tot.Rx != 2200 || tot.Tx != 500 {
		t.Fatalf("totals: %+v %v", tot, err)
	}
	series, err := st.TrafficSeries("n1", t0.Add(-time.Hour), false)
	if err != nil || len(series) == 0 {
		t.Fatalf("series: %v %v", series, err)
	}
	if err := st.RollupAndPrune(t0.Add(3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	hours, err := st.TrafficSeries("n1", t0.Add(-2*time.Hour), true)
	if err != nil || len(hours) != 1 || hours[0].Rx != 2200 {
		t.Fatalf("hourly: %+v %v", hours, err)
	}
}
