package master

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	geoCacheTTL      = 24 * time.Hour
	geoOnlinePerMin  = 40 // ip-api.com free tier allows 45/min
	geoOnlineTimeout = 5 * time.Second
	geoOnlineURL     = "http://ip-api.com/json/%s?fields=status,country,countryCode"
)

// GeoIP resolves public IPs to a country: offline mmdb first, then the
// rate-limited ip-api.com fallback, with results cached in SQLite.
type GeoIP struct {
	store  *Store
	db     *maxminddb.Reader
	online bool
	client *http.Client
	// lookupURL is a format string taking the IP; overridable in tests.
	lookupURL string

	mu       sync.Mutex
	windowAt time.Time
	used     int
	inflight map[string]*geoCall
}

type geoCall struct {
	done          chan struct{}
	country, code string
	err           error
}

type GeoResult struct {
	Country     string
	CountryCode string
}

func NewGeoIP(store *Store, dataDir, dbPath string, online bool) *GeoIP {
	g := &GeoIP{
		store:     store,
		online:    online,
		client:    &http.Client{Timeout: geoOnlineTimeout},
		lookupURL: geoOnlineURL,
		inflight:  make(map[string]*geoCall),
	}
	candidates := []string{dbPath}
	if dbPath == "" {
		candidates = []string{
			filepath.Join(dataDir, "GeoLite2-Country.mmdb"),
			filepath.Join(dataDir, "dbip-country-lite.mmdb"),
		}
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if db, err := maxminddb.Open(p); err == nil {
			g.db = db
			fmt.Fprintf(os.Stderr, "wireguard-go master: geoip database %s\n", p)
			break
		} else if dbPath != "" {
			fmt.Fprintf(os.Stderr, "wireguard-go master: geoip database %s: %v\n", p, err)
		}
	}
	return g
}

func (g *GeoIP) Close() {
	if g.db != nil {
		_ = g.db.Close()
	}
}

type mmdbCountry struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"registered_country"`
}

func (g *GeoIP) lookupOffline(a netip.Addr) (country, code string, ok bool) {
	if g.db == nil {
		return "", "", false
	}
	var rec mmdbCountry
	if err := g.db.Lookup(net.IP(a.AsSlice()), &rec); err != nil {
		return "", "", false
	}
	code, names := rec.Country.ISOCode, rec.Country.Names
	if code == "" {
		code, names = rec.RegisteredCountry.ISOCode, rec.RegisteredCountry.Names
	}
	if code == "" {
		return "", "", false
	}
	country = names["en"]
	if country == "" {
		country = code
	}
	return country, code, true
}

// allowOnline enforces a fixed one-minute window budget.
func (g *GeoIP) allowOnline(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.windowAt) >= time.Minute {
		g.windowAt = now
		g.used = 0
	}
	if g.used >= geoOnlinePerMin {
		return false
	}
	g.used++
	return true
}

func (g *GeoIP) lookupOnline(ctx context.Context, ip string) (country, code string, err error) {
	if !g.allowOnline(time.Now()) {
		return "", "", fmt.Errorf("geoip online rate limit")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(g.lookupURL, ip), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var out struct {
		Status      string `json:"status"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&out); err != nil {
		return "", "", err
	}
	if out.Status != "success" || out.CountryCode == "" {
		return "", "", fmt.Errorf("geoip online: status %q", out.Status)
	}
	return out.Country, out.CountryCode, nil
}

// Lookup resolves one IP. Concurrent lookups of the same IP share one call.
func (g *GeoIP) Lookup(ctx context.Context, ipStr string) (GeoResult, error) {
	a, err := netip.ParseAddr(ipStr)
	if err != nil {
		return GeoResult{}, err
	}
	a = a.Unmap()
	if !isPublicAddr(a) {
		return GeoResult{}, fmt.Errorf("not a public address")
	}
	ip := a.String()
	if country, code, ok := g.store.GeoCacheGet(ip, geoCacheTTL); ok {
		return GeoResult{country, code}, nil
	}
	if country, code, ok := g.lookupOffline(a); ok {
		_ = g.store.GeoCachePut(ip, country, code)
		return GeoResult{country, code}, nil
	}
	if !g.online {
		return GeoResult{}, fmt.Errorf("no geoip source for %s", ip)
	}

	g.mu.Lock()
	// Re-check under the lock: a finished call caches before leaving inflight.
	if country, code, ok := g.store.GeoCacheGet(ip, geoCacheTTL); ok {
		g.mu.Unlock()
		return GeoResult{country, code}, nil
	}
	if call, ok := g.inflight[ip]; ok {
		g.mu.Unlock()
		select {
		case <-call.done:
			return GeoResult{call.country, call.code}, call.err
		case <-ctx.Done():
			return GeoResult{}, ctx.Err()
		}
	}
	call := &geoCall{done: make(chan struct{})}
	g.inflight[ip] = call
	g.mu.Unlock()

	call.country, call.code, call.err = g.lookupOnline(ctx, ip)
	if call.err == nil {
		_ = g.store.GeoCachePut(ip, call.country, call.code)
	}
	g.mu.Lock()
	delete(g.inflight, ip)
	g.mu.Unlock()
	close(call.done)
	return GeoResult{call.country, call.code}, call.err
}

// ResolveNode looks up the node's public IPv4 (then IPv6) and stores the
// country on the node row.
func (g *GeoIP) ResolveNode(nodeID, v4, v6 string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*geoOnlineTimeout)
	defer cancel()
	for _, ip := range []string{v4, v6} {
		if ip == "" {
			continue
		}
		res, err := g.Lookup(ctx, ip)
		if err != nil {
			continue
		}
		_ = g.store.SetNodeGeo(nodeID, res.Country, res.CountryCode, time.Now().UTC())
		return
	}
}
