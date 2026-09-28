package api

import (
	"net/http"
	"sync"

	"golang.zx2c4.com/wireguard/src/master/hub"
	"golang.zx2c4.com/wireguard/src/update"
)

const upgradeFanout = 16

type upgradeRequest struct {
	Master   bool     `json:"master"`
	NodeIDs  []string `json:"nodeIds"`
	Outdated bool     `json:"outdated"` // every online agent older than the latest release
	Force    bool     `json:"force"`
}

type upgradeResult struct {
	NodeID  string `json:"nodeId,omitempty"`
	Name    string `json:"name,omitempty"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type upgradeResponse struct {
	Master *upgradeResult  `json:"master,omitempty"`
	Agents []upgradeResult `json:"agents"`
}

// handleUpgrade starts self-updates from the web UI: the Master's own binary
// and/or connected agents. Each target only confirms that its updater started;
// new versions show up once the processes restart.
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	var req upgradeRequest
	if err := readJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ids := req.NodeIDs
	if req.Outdated {
		ids = append(ids, s.outdatedAgentIDs()...)
	}
	resp := upgradeResponse{Agents: s.upgradeAgents(dedupe(ids), req.Force)}
	// Master last: its updater may restart this process.
	if req.Master {
		resp.Master = s.upgradeMaster(req.Force)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) upgradeMaster(force bool) *upgradeResult {
	st := s.updates.Status()
	if !force && st.Latest != "" && !update.Newer(st.Latest, st.Current) {
		return &upgradeResult{OK: false, Message: "已是最新版本"}
	}
	note, err := update.SpawnDetached(force)
	if err != nil {
		return &upgradeResult{OK: false, Message: err.Error()}
	}
	return &upgradeResult{OK: true, Message: note}
}

func (s *Server) upgradeAgents(ids []string, force bool) []upgradeResult {
	out := make([]upgradeResult, len(ids))
	m := s.store.Snapshot()
	sem := make(chan struct{}, upgradeFanout)
	var wg sync.WaitGroup
	for i, id := range ids {
		name := id
		if n := m.FindNode(id); n != nil && n.Name != "" {
			name = n.Name
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ver, online := s.hub.AgentVersion(id); online && ver == "" {
				out[i] = upgradeResult{NodeID: id, Name: name, Message: hub.ErrUpdateNoAck.Error()}
				return
			}
			ack, err := s.hub.RequestUpdate(id, force)
			res := upgradeResult{NodeID: id, Name: name, OK: err == nil && ack.OK, Message: ack.Message}
			if err != nil {
				res.Message = err.Error()
			}
			out[i] = res
		}()
	}
	wg.Wait()
	return out
}

func (s *Server) outdatedAgentIDs() []string {
	target := s.updates.Status().Latest
	if target == "" {
		return nil
	}
	var ids []string
	for id, ver := range s.hub.AgentVersions() {
		if ver == "" || update.Newer(target, ver) {
			ids = append(ids, id)
		}
	}
	return ids
}

func dedupe(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok || id == "" {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
