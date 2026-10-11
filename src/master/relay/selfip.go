package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"time"
)

const publicIPTimeout = 5 * time.Second

// publicIPv4URL is GET JSON {"status":"success","query":"<ipv4>"}; tests override it.
var publicIPv4URL = "http://ip-api.com/json/?fields=status,query"

func (s *Service) refreshPublicIP() bool {
	if s.Store == nil {
		return false
	}
	v4 := lookupPublicV4()
	if v4 == "" {
		return false
	}
	changed, err := s.Store.SetRelayPublicV4(v4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lasitan-cluster master: relay public ip: %v\n", err)
		return false
	}
	return changed
}

func lookupPublicV4() string {
	client := &http.Client{Timeout: publicIPTimeout}
	resp, err := client.Get(publicIPv4URL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Status string `json:"status"`
		Query  string `json:"query"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return ""
	}
	if out.Status != "" && out.Status != "success" {
		return ""
	}
	a, err := netip.ParseAddr(out.Query)
	if err != nil {
		return ""
	}
	a = a.Unmap()
	if !a.Is4() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsMulticast() || a.IsLoopback() {
		return ""
	}
	return a.String()
}
