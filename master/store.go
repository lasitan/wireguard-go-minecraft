package master

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.zx2c4.com/wireguard/meshcfg"
)

type Store struct {
	mu   sync.Mutex
	path string
	mesh meshcfg.Mesh
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "mesh.json")
	s := &Store{path: path, mesh: meshcfg.EmptyMesh()}
	if _, err := os.Stat(path); err == nil {
		if err := meshcfg.LoadJSON(path, &s.mesh); err != nil {
			return nil, fmt.Errorf("load mesh: %w", err)
		}
	} else if os.IsNotExist(err) {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return s, nil
}

func (s *Store) Snapshot() meshcfg.Mesh {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMesh(s.mesh)
}

func (s *Store) PutMesh(m meshcfg.Mesh) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.Revision = s.mesh.Revision + 1
	if err := validateMesh(m); err != nil {
		return err
	}
	s.mesh = m
	return s.persistLocked()
}

func (s *Store) AddNode(n meshcfg.Node) (meshcfg.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Agent identity is always a Master-assigned UUID; clients cannot pick the id.
	id, err := meshcfg.GenerateNodeID()
	if err != nil {
		return n, err
	}
	n.ID = id
	if s.mesh.FindNode(n.ID) != nil {
		return n, fmt.Errorf("node %q already exists", n.ID)
	}
	if n.Role != meshcfg.RoleServer && n.Role != meshcfg.RoleClient {
		return n, fmt.Errorf("role must be server or client")
	}
	if n.PrivateKey == "" || n.PublicKey == "" {
		priv, pub, err := meshcfg.GenerateKeyPair()
		if err != nil {
			return n, err
		}
		n.PrivateKey, n.PublicKey = priv, pub
	}
	if n.Token == "" {
		tok, err := meshcfg.GenerateToken()
		if err != nil {
			return n, err
		}
		n.Token = tok
	}
	s.mesh.Nodes = append(s.mesh.Nodes, n)
	s.mesh.Revision++
	if err := s.persistLocked(); err != nil {
		return n, err
	}
	return n, nil
}

func (s *Store) DesiredForToken(token string) (*meshcfg.DesiredConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.mesh.FindNodeByToken(token)
	if n == nil {
		return nil, errUnauthorized
	}
	return meshcfg.CompileDesired(&s.mesh, n.ID)
}

func (s *Store) DesiredForNode(nodeID string) (*meshcfg.DesiredConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return meshcfg.CompileDesired(&s.mesh, nodeID)
}

func (s *Store) persistLocked() error {
	return meshcfg.SaveJSON(s.path, s.mesh, 0600)
}

var errUnauthorized = fmt.Errorf("unauthorized")

func validateMesh(m meshcfg.Mesh) error {
	ids := make(map[string]struct{}, len(m.Nodes))
	for _, n := range m.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node missing id")
		}
		if !meshcfg.ValidNodeID(n.ID) {
			return fmt.Errorf("node id %q must be a UUID", n.ID)
		}
		if _, ok := ids[n.ID]; ok {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = struct{}{}
		if n.Role != meshcfg.RoleServer && n.Role != meshcfg.RoleClient {
			return fmt.Errorf("node %s: invalid role", n.ID)
		}
		if n.Address == "" {
			return fmt.Errorf("node %s: address required", n.ID)
		}
		if n.PublicKey == "" || n.PrivateKey == "" {
			return fmt.Errorf("node %s: keys required", n.ID)
		}
		if n.Token == "" {
			return fmt.Errorf("node %s: token required", n.ID)
		}
	}
	for _, l := range m.Links {
		if _, ok := ids[l.FromNodeID]; !ok {
			return fmt.Errorf("link from unknown %q", l.FromNodeID)
		}
		if _, ok := ids[l.ToNodeID]; !ok {
			return fmt.Errorf("link to unknown %q", l.ToNodeID)
		}
	}
	for _, f := range m.Forwards {
		if _, ok := ids[f.NodeID]; !ok {
			return fmt.Errorf("forward on unknown %q", f.NodeID)
		}
		if _, ok := ids[f.DestNodeID]; !ok {
			return fmt.Errorf("forward dest unknown %q", f.DestNodeID)
		}
	}
	return nil
}

func cloneMesh(m meshcfg.Mesh) meshcfg.Mesh {
	b, _ := json.Marshal(m)
	var out meshcfg.Mesh
	_ = json.Unmarshal(b, &out)
	return out
}
