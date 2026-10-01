package api

import (
	"net/http"

	"golang.zx2c4.com/wireguard/src/master/stats"
	"golang.zx2c4.com/wireguard/src/master/store"
	"golang.zx2c4.com/wireguard/src/update"
)

type AgentInstallCommands = agentInstallCommands

func MasterPublicURL(r *http.Request) string {
	return masterPublicURL(r)
}

func ClientIP(r *http.Request) string {
	return clientIP(r)
}

func IsCloudflareIPString(ip string) bool {
	addr, ok := parseIPString(ip)
	return ok && isCloudflareIP(addr)
}

func IsBanableAuthIP(ip string) bool {
	return isBanableAuthIP(ip)
}

type NodeStatsView = nodeStatsView
type VersionView = versionView

func (s *Server) Store() *store.Store {
	return s.store
}

func (s *Server) Stats() *stats.StatsService {
	return s.stats
}

func (s *Server) Updates() *update.Checker {
	return s.updates
}

// RunUI starts the admin UI push loop (ListenAndServe does this in production).
func (s *Server) RunUI(stop <-chan struct{}) {
	s.ui.run(stop)
}
