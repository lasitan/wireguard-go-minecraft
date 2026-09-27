package master

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeoIPOnlineFallbackAndCache(t *testing.T) {
	st := openTestStore(t, "10.10.0.0/24")

	var hits atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, `{"status":"success","country":"Japan","countryCode":"JP"}`)
	}))
	defer api.Close()

	g := NewGeoIP(st, t.TempDir(), "", true)
	g.lookupURL = api.URL + "/json/%s"
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := g.Lookup(ctx, "8.8.8.8")
			if err != nil || res.CountryCode != "JP" {
				t.Errorf("lookup: %+v %v", res, err)
			}
		}()
	}
	wg.Wait()
	if res, err := g.Lookup(ctx, "8.8.8.8"); err != nil || res.Country != "Japan" {
		t.Fatalf("cached lookup: %+v %v", res, err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("online hits = %d, want 1 (dedupe + cache)", n)
	}

	if _, err := g.Lookup(ctx, "192.168.1.1"); err == nil {
		t.Fatal("private address must not be looked up")
	}
}

func TestGeoIPRateLimit(t *testing.T) {
	g := &GeoIP{}
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < geoOnlinePerMin; i++ {
		if !g.allowOnline(now) {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if g.allowOnline(now) {
		t.Fatal("budget exceeded but allowed")
	}
	if !g.allowOnline(now.Add(61e9)) {
		t.Fatal("new window should allow")
	}
}
