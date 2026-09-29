package geoip

import "time"

const GeoOnlinePerMin = geoOnlinePerMin

func (g *GeoIP) SetLookupURL(url string) {
	g.lookupURL = url
}

func (g *GeoIP) AllowOnline(now time.Time) bool {
	return g.allowOnline(now)
}
