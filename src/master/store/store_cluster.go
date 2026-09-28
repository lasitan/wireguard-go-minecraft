package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"golang.zx2c4.com/wireguard/src/core"
)

// Clusters: cards snapped side by side. Members share their magnet config
// (listen port, routes, mothers, forwards); editing one member rewrites the
// others. Children of a member mother are served by every member (see
// core.PlanMesh) and forwards toward an offline member move to a live one
// (Mesh.Standby, maintained by pathsel).

const (
	maxClusterSize = 8
	metaStandby    = "standby_json"
)

type clusterSync struct {
	port, routes, parents, forwards bool
}

var syncAll = clusterSync{port: true, routes: true, parents: true, forwards: true}

func newClusterID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "c" + hex.EncodeToString(b[:])
}

// joinCluster puts id into peerID's cluster and copies peerID's config onto it.
func joinCluster(m *core.Mesh, id, peerID string) error {
	n, peer := m.FindNode(id), m.FindNode(peerID)
	if n == nil || peer == nil || id == peerID {
		return fmt.Errorf("cluster peer %q not found", peerID)
	}
	if n.IsMagnetParent() != peer.IsMagnetParent() {
		return fmt.Errorf("只能与同类卡片组成集群（母卡配母卡，普通卡配普通卡）")
	}
	if peer.Cluster != "" && peer.Cluster == n.Cluster {
		return nil
	}
	if peer.Cluster == "" {
		peer.Cluster = newClusterID()
	}
	if len(m.ClusterMembers(peer.Cluster)) >= maxClusterSize {
		return fmt.Errorf("a cluster holds at most %d cards", maxClusterSize)
	}
	n.Cluster = peer.Cluster
	return copyClusterConfig(m, peerID, id, syncAll)
}

func leaveCluster(n *core.Node) {
	n.Cluster = ""
}

// syncCluster copies the selected config of srcID onto every other member.
func syncCluster(m *core.Mesh, srcID string, what clusterSync) error {
	src := m.FindNode(srcID)
	if src == nil || src.Cluster == "" {
		return nil
	}
	for _, id := range m.ClusterMembers(src.Cluster) {
		if id == srcID {
			continue
		}
		if err := copyClusterConfig(m, srcID, id, what); err != nil {
			return err
		}
	}
	return nil
}

func copyClusterConfig(m *core.Mesh, srcID, dstID string, what clusterSync) error {
	src, dst := m.FindNode(srcID), m.FindNode(dstID)
	if src == nil || dst == nil {
		return nil
	}
	if what.port && dst.ListenPort != src.ListenPort {
		setListenPort(m, dst, src.ListenPort)
	}
	if what.routes {
		routes, err := clusterRoutes(src, dst)
		if err != nil {
			return err
		}
		dst.Routes = routes
	}
	if what.parents && !src.IsMagnetParent() {
		var parents []string
		for _, l := range m.Links {
			if l.FromNodeID != srcID || l.ToNodeID == dstID {
				continue
			}
			if p := m.FindNode(l.ToNodeID); p != nil && p.IsMagnetParent() {
				parents = append(parents, l.ToNodeID)
			}
		}
		if err := setParents(m, dstID, parents); err != nil {
			return err
		}
	}
	if what.forwards {
		out := m.Forwards[:0:0]
		for _, f := range m.Forwards {
			if f.NodeID != dstID {
				out = append(out, f)
			}
		}
		for _, f := range m.Forwards {
			if f.NodeID != srcID {
				continue
			}
			f.NodeID = dstID
			if f.DestNodeID == srcID {
				f.DestNodeID = dstID
			}
			out = append(out, f)
		}
		m.Forwards = out
	}
	return nil
}

// clusterRoutes is src's route list with src's own VPN subnet swapped for dst's.
func clusterRoutes(src, dst *core.Node) ([]string, error) {
	sp, err1 := core.VPNPrefixFromAddress(src.Address)
	dp, err2 := core.VPNPrefixFromAddress(dst.Address)
	if err1 != nil || err2 != nil {
		return core.NormalizeRouteCIDRs(src.Routes)
	}
	return core.ReplaceVPNRoute(src.Routes, sp, dp)
}

// pruneClusters dissolves clusters left with a single member.
func pruneClusters(m *core.Mesh) {
	count := map[string]int{}
	for _, n := range m.Nodes {
		if n.Cluster != "" {
			count[n.Cluster]++
		}
	}
	for i := range m.Nodes {
		if c := m.Nodes[i].Cluster; c != "" && count[c] < 2 {
			m.Nodes[i].Cluster = ""
		}
	}
}

func (s *Store) standbyLocked() map[string]string {
	raw, err := s.metaGet(metaStandby)
	if err != nil || raw == "" {
		return nil
	}
	var out map[string]string
	if json.Unmarshal([]byte(raw), &out) != nil || len(out) == 0 {
		return nil
	}
	return out
}

// SetStandby stores which live cluster member covers each offline one and
// bumps the revision when that changed. Reports whether anything changed.
func (s *Store) SetStandby(sb map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := []byte("{}")
	if len(sb) > 0 {
		b, err := json.Marshal(sb)
		if err != nil {
			return false, err
		}
		next = b
	}
	prev, _ := s.metaGet(metaStandby)
	if strings.TrimSpace(prev) == "" {
		prev = "{}"
	}
	if bytes.Equal([]byte(prev), next) {
		return false, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaStandby, string(next)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = ?`, metaRevision); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
