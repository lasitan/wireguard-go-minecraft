package api

import (
	"net/http"

	"golang.zx2c4.com/wireguard/src/update"
)

type updateCommands struct {
	Linux     string `json:"linux"`
	LinuxCN   string `json:"linuxCn"`
	Installed string `json:"installed"`
	Windows   string `json:"windows"`
}

type outdatedAgent struct {
	NodeID  string `json:"nodeId"`
	Name    string `json:"name"`
	Version string `json:"version"` // "" = pre-2.0.3 agent without a build version
}

type versionView struct {
	update.Status
	ReleasesURL    string          `json:"releasesUrl"`
	Commands       updateCommands  `json:"commands"`
	OutdatedAgents []outdatedAgent `json:"outdatedAgents"`
}

func updateCommandSet() updateCommands {
	return updateCommands{
		Linux:     "curl -fsSL " + update.InstallScriptURL + " | sudo bash",
		LinuxCN:   "curl -fsSL https://ghfast.top/" + update.InstallScriptURL + " | sudo WG_MC_GH_PROXY=https://ghfast.top/ bash",
		Installed: "sudo wireguard-go update",
		Windows:   `powershell -ExecutionPolicy Bypass -c "irm ` + update.InstallPS1URL + ` | iex"`,
	}
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		// POST = "check now"; the result lands on the next GET.
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		s.updates.Refresh()
	}
	st := s.updates.Status()
	target := st.Latest
	if target == "" || update.Newer(st.Current, target) {
		target = st.Current
	}

	v := versionView{
		Status:         st,
		ReleasesURL:    update.ReleasesURL,
		Commands:       updateCommandSet(),
		OutdatedAgents: []outdatedAgent{},
	}
	if target != "" {
		m := s.store.Snapshot()
		for id, ver := range s.hub.AgentVersions() {
			if ver != "" && !update.Newer(target, ver) {
				continue
			}
			name := id
			if n := m.FindNode(id); n != nil {
				name = n.Name
			}
			v.OutdatedAgents = append(v.OutdatedAgents, outdatedAgent{NodeID: id, Name: name, Version: ver})
		}
	}
	writeJSON(w, http.StatusOK, v)
}
