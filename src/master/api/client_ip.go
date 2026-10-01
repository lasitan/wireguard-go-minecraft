package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the caller's IP as seen by Master.
//
// Trust rules:
//   - Direct peer is Cloudflare edge → trust CF-Connecting-IP only
//     (covers Pseudo IPv4 → Overwrite Headers, which rewrites that header
//     to a Class-E pseudo address; never fall back to banning the edge IP).
//   - Direct peer is loopback/private → trust CF-Connecting-IP → X-Real-IP →
//     X-Forwarded-For (first hop), for local reverse proxies in front of Master.
//   - Otherwise (direct public access) → ignore all forwarded headers to
//     block client-injected CF-Connecting-IP / XFF spoofing.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return strings.TrimSpace(host)
	}
	ip = ip.Unmap()

	switch {
	case isCloudflareIP(ip):
		if h := headerIP(r, "CF-Connecting-IP"); h != "" {
			return h
		}
		// Missing header: keep edge IP for logging, but auth ban path
		// refuses Cloudflare addresses via isBanableAuthIP.
		return ip.String()
	case ip.IsLoopback() || ip.IsPrivate():
		if h := headerIP(r, "CF-Connecting-IP"); h != "" {
			return h
		}
		if h := headerIP(r, "X-Real-IP"); h != "" {
			return h
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if fip, err := netip.ParseAddr(first); err == nil {
				return fip.Unmap().String()
			}
		}
	}
	return ip.String()
}

func headerIP(r *http.Request, name string) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if v == "" {
		return ""
	}
	// Some CDNs append ports; strip if present.
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	ip, err := netip.ParseAddr(v)
	if err != nil {
		return ""
	}
	return ip.Unmap().String()
}

// enrollSourceHost is the agent's public IP as seen by Master (CDN-aware).
func enrollSourceHost(r *http.Request) string {
	return clientIP(r)
}
