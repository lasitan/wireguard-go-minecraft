package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const whoamiTimeout = 5 * time.Second

// detectPublicIPs asks Master which source address it sees, once over IPv4
// and once over IPv6. Non-public results (LAN masters) are discarded.
func detectPublicIPs(masterURL string) (v4, v6 netip.Addr) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); v4 = whoami(masterURL, "tcp4") }()
	go func() { defer wg.Done(); v6 = whoami(masterURL, "tcp6") }()
	wg.Wait()
	return v4, v6
}

func whoami(masterURL, network string) netip.Addr {
	dialer := &net.Dialer{Timeout: whoamiTimeout}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	client := &http.Client{Timeout: whoamiTimeout, Transport: tr}
	defer tr.CloseIdleConnections()

	resp, err := client.Get(stringsTrimSlash(masterURL) + "/api/agent/whoami")
	if err != nil {
		return netip.Addr{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}
	}
	var out struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return netip.Addr{}
	}
	a, err := netip.ParseAddr(out.IP)
	if err != nil {
		return netip.Addr{}
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return netip.Addr{}
	}
	if network == "tcp4" && !a.Is4() || network == "tcp6" && !a.Is6() {
		return netip.Addr{}
	}
	return a
}
