package master

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/meshcfg"

	_ "modernc.org/sqlite"
)

const (
	metaRevision      = "revision"
	metaEnrollToken   = "enroll_token"
	metaVPNSubnet     = "vpn_subnet"
	metaDefaultIface  = "default_iface"
	metaDefaultPoll   = "default_poll"
	metaTransportJSON = "transport_json"
)

type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

type storeSettings struct {
	EnrollToken   string
	VPNSubnet     string
	DefaultIface  string
	DefaultPoll   string
	TransportJSON json.RawMessage
}

func OpenStore(dataDir string, seedEnroll, seedSubnet string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "mesh.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.ensureSeeds(seedEnroll, seedSubnet); err != nil {
		_ = db.Close()
		return nil, err
	}
	// One-time import of legacy mesh.json if DB has no nodes.
	legacy := filepath.Join(dataDir, "mesh.json")
	if err := s.importLegacyJSON(legacy); err != nil {
		fmt.Fprintf(os.Stderr, "wireguard-go master: legacy mesh.json import: %v\n", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY NOT NULL,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  public_key TEXT NOT NULL,
  private_key TEXT NOT NULL,
  address TEXT NOT NULL,
  listen_port INTEGER NOT NULL DEFAULT 0,
  endpoint TEXT NOT NULL DEFAULT '',
  mtu INTEGER NOT NULL DEFAULT 0,
  token TEXT NOT NULL UNIQUE,
  iface_name TEXT NOT NULL DEFAULT '',
  poll_interval TEXT NOT NULL DEFAULT '',
  transport_json TEXT NOT NULL DEFAULT '',
  last_seen TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  from_node_id TEXT NOT NULL,
  to_node_id TEXT NOT NULL,
  allowed_ips TEXT NOT NULL DEFAULT '[]',
  keepalive INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS forwards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  node_id TEXT NOT NULL,
  protocol TEXT NOT NULL,
  listen TEXT NOT NULL,
  dest_node_id TEXT NOT NULL,
  dest_port INTEGER NOT NULL
);
`)
	return err
}

func (s *Store) ensureSeeds(seedEnroll, seedSubnet string) error {
	if seedEnroll == "" {
		seedEnroll = "change-me-enroll"
	}
	if seedSubnet == "" {
		seedSubnet = "10.10.0.0/24"
	}
	defaults := map[string]string{
		metaRevision:     "0",
		metaEnrollToken:  seedEnroll,
		metaVPNSubnet:    seedSubnet,
		metaDefaultIface: "wg0",
		metaDefaultPoll:  "10s",
		metaTransportJSON: `{
  "tcp": {
    "dialTimeout": "3s",
    "reconnectInitialBackoff": "1s",
    "reconnectMaxBackoff": "60s",
    "rxIdleTimeout": "5s"
  },
  "mc": {
    "enabled": true,
    "handshakeTimeout": "10s",
    "deepCamouflage": true,
    "loginUsername": "Steve",
    "loginPluginChannel": "minecraft:register",
    "loginPluginSecret": "change-me-shared-secret"
  }
}`,
	}
	for k, v := range defaults {
		var existing string
		err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, k).Scan(&existing)
		if err == sql.ErrNoRows {
			if _, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)`, k, v); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) importLegacyJSON(path string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m meshcfg.Mesh
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaceMeshLocked(m)
}

func (s *Store) metaGet(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v, err
}

func (s *Store) metaSet(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Settings() (storeSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsLocked()
}

func (s *Store) settingsLocked() (storeSettings, error) {
	out := storeSettings{}
	var err error
	out.EnrollToken, err = s.metaGet(metaEnrollToken)
	if err != nil {
		return out, err
	}
	out.VPNSubnet, err = s.metaGet(metaVPNSubnet)
	if err != nil {
		return out, err
	}
	out.DefaultIface, err = s.metaGet(metaDefaultIface)
	if err != nil {
		return out, err
	}
	out.DefaultPoll, err = s.metaGet(metaDefaultPoll)
	if err != nil {
		return out, err
	}
	tr, err := s.metaGet(metaTransportJSON)
	if err != nil {
		return out, err
	}
	out.TransportJSON = json.RawMessage(tr)
	return out, nil
}

func (s *Store) Snapshot() meshcfg.Mesh {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return meshcfg.EmptyMesh()
	}
	return m
}

func (s *Store) loadMeshLocked() (meshcfg.Mesh, error) {
	m := meshcfg.EmptyMesh()
	revStr, err := s.metaGet(metaRevision)
	if err != nil {
		return m, err
	}
	fmt.Sscanf(revStr, "%d", &m.Revision)

	rows, err := s.db.Query(`SELECT id, name, role, public_key, private_key, address, listen_port, endpoint, mtu, token, iface_name, poll_interval, transport_json, last_seen FROM nodes`)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var n meshcfg.Node
		var listenPort, mtu int
		var transport, lastSeen string
		if err := rows.Scan(&n.ID, &n.Name, &n.Role, &n.PublicKey, &n.PrivateKey, &n.Address, &listenPort, &n.Endpoint, &mtu, &n.Token, &n.Interface, &n.PollInterval, &transport, &lastSeen); err != nil {
			return m, err
		}
		n.ListenPort = uint16(listenPort)
		n.MTU = mtu
		if transport != "" {
			n.Transport = json.RawMessage(transport)
		}
		if lastSeen != "" {
			if t, e := time.Parse(time.RFC3339Nano, lastSeen); e == nil {
				n.LastSeen = t
			}
		}
		m.Nodes = append(m.Nodes, n)
	}

	lrows, err := s.db.Query(`SELECT from_node_id, to_node_id, allowed_ips, keepalive FROM links`)
	if err != nil {
		return m, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l meshcfg.Link
		var allowed string
		if err := lrows.Scan(&l.FromNodeID, &l.ToNodeID, &allowed, &l.Keepalive); err != nil {
			return m, err
		}
		_ = json.Unmarshal([]byte(allowed), &l.AllowedIPs)
		m.Links = append(m.Links, l)
	}

	frows, err := s.db.Query(`SELECT node_id, protocol, listen, dest_node_id, dest_port FROM forwards`)
	if err != nil {
		return m, err
	}
	defer frows.Close()
	for frows.Next() {
		var f meshcfg.Forward
		var port int
		if err := frows.Scan(&f.NodeID, &f.Protocol, &f.Listen, &f.DestNodeID, &port); err != nil {
			return m, err
		}
		f.DestPort = uint16(port)
		m.Forwards = append(m.Forwards, f)
	}
	return m, nil
}

func (s *Store) PutMesh(m meshcfg.Mesh) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.loadMeshLocked()
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(cur.Nodes))
	for _, n := range cur.Nodes {
		existing[n.ID] = struct{}{}
	}
	for _, n := range m.Nodes {
		if _, ok := existing[n.ID]; !ok {
			return fmt.Errorf("cannot add node %q via mesh edit; agents must enroll", n.ID)
		}
	}
	m.Revision = cur.Revision + 1
	if err := validateMesh(m); err != nil {
		return err
	}
	return s.replaceMeshLocked(m)
}

func (s *Store) replaceMeshLocked(m meshcfg.Mesh) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM forwards`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM links`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM nodes`); err != nil {
		return err
	}
	for _, n := range m.Nodes {
		tr := ""
		if len(n.Transport) > 0 {
			tr = string(n.Transport)
		}
		last := ""
		if !n.LastSeen.IsZero() {
			last = n.LastSeen.UTC().Format(time.RFC3339Nano)
		}
		if _, err := tx.Exec(`INSERT INTO nodes(id, name, role, public_key, private_key, address, listen_port, endpoint, mtu, token, iface_name, poll_interval, transport_json, last_seen)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			n.ID, n.Name, n.Role, n.PublicKey, n.PrivateKey, n.Address, int(n.ListenPort), n.Endpoint, n.MTU, n.Token, n.Interface, n.PollInterval, tr, last); err != nil {
			return err
		}
	}
	for _, l := range m.Links {
		allowed, _ := json.Marshal(l.AllowedIPs)
		if allowed == nil {
			allowed = []byte("[]")
		}
		if _, err := tx.Exec(`INSERT INTO links(from_node_id, to_node_id, allowed_ips, keepalive) VALUES(?,?,?,?)`,
			l.FromNodeID, l.ToNodeID, string(allowed), l.Keepalive); err != nil {
			return err
		}
	}
	for _, f := range m.Forwards {
		if _, err := tx.Exec(`INSERT INTO forwards(node_id, protocol, listen, dest_node_id, dest_port) VALUES(?,?,?,?,?)`,
			f.NodeID, f.Protocol, f.Listen, f.DestNodeID, int(f.DestPort)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaRevision, fmt.Sprintf("%d", m.Revision)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Enroll(name, role, endpoint string, listenPort uint16) (meshcfg.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == "" {
		role = meshcfg.RoleClient
	}
	if role != meshcfg.RoleServer && role != meshcfg.RoleClient {
		return meshcfg.Node{}, fmt.Errorf("role must be server or client")
	}
	settings, err := s.settingsLocked()
	if err != nil {
		return meshcfg.Node{}, err
	}
	m, err := s.loadMeshLocked()
	if err != nil {
		return meshcfg.Node{}, err
	}
	id, err := meshcfg.GenerateNodeID()
	if err != nil {
		return meshcfg.Node{}, err
	}
	priv, pub, err := meshcfg.GenerateKeyPair()
	if err != nil {
		return meshcfg.Node{}, err
	}
	tok, err := meshcfg.GenerateToken()
	if err != nil {
		return meshcfg.Node{}, err
	}
	addr, err := nextAddress(settings.VPNSubnet, m.Nodes)
	if err != nil {
		return meshcfg.Node{}, err
	}
	n := meshcfg.Node{
		ID:           id,
		Name:         name,
		Role:         role,
		PublicKey:    pub,
		PrivateKey:   priv,
		Address:      addr,
		ListenPort:   listenPort,
		Endpoint:     endpoint,
		Token:        tok,
		Interface:    settings.DefaultIface,
		PollInterval: settings.DefaultPoll,
		LastSeen:     time.Now().UTC(),
	}
	if role == meshcfg.RoleServer && n.ListenPort == 0 {
		n.ListenPort = 25590
	}
	m.Nodes = append(m.Nodes, n)
	if role == meshcfg.RoleClient {
		for _, peer := range m.Nodes {
			if peer.ID == n.ID || peer.Role != meshcfg.RoleServer {
				continue
			}
			m.Links = append(m.Links, meshcfg.Link{FromNodeID: n.ID, ToNodeID: peer.ID, Keepalive: 5})
		}
	} else {
		for _, peer := range m.Nodes {
			if peer.ID == n.ID || peer.Role != meshcfg.RoleClient {
				continue
			}
			m.Links = append(m.Links, meshcfg.Link{FromNodeID: peer.ID, ToNodeID: n.ID, Keepalive: 5})
		}
	}
	m.Revision++
	if err := validateMesh(m); err != nil {
		return meshcfg.Node{}, err
	}
	if err := s.replaceMeshLocked(m); err != nil {
		return meshcfg.Node{}, err
	}
	return n, nil
}

func (s *Store) DeleteNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return err
	}
	idx := -1
	for i := range m.Nodes {
		if m.Nodes[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("node %q not found", id)
	}
	m.Nodes = append(m.Nodes[:idx], m.Nodes[idx+1:]...)
	var links []meshcfg.Link
	for _, l := range m.Links {
		if l.FromNodeID == id || l.ToNodeID == id {
			continue
		}
		links = append(links, l)
	}
	m.Links = links
	var forwards []meshcfg.Forward
	for _, f := range m.Forwards {
		if f.NodeID == id || f.DestNodeID == id {
			continue
		}
		forwards = append(forwards, f)
	}
	m.Forwards = forwards
	m.Revision++
	return s.replaceMeshLocked(m)
}

func (s *Store) DesiredForToken(token string) (*meshcfg.DesiredConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return nil, err
	}
	n := m.FindNodeByToken(token)
	if n == nil {
		return nil, errUnauthorized
	}
	n.LastSeen = time.Now().UTC()
	_, _ = s.db.Exec(`UPDATE nodes SET last_seen = ? WHERE id = ?`, n.LastSeen.UTC().Format(time.RFC3339Nano), n.ID)
	settings, err := s.settingsLocked()
	if err != nil {
		return nil, err
	}
	return meshcfg.CompileDesired(&m, n.ID, meshcfg.DesiredDefaults{
		InterfaceName: settings.DefaultIface,
		PollInterval:  settings.DefaultPoll,
		Transport:     settings.TransportJSON,
	})
}

func (s *Store) DesiredForNode(nodeID string) (*meshcfg.DesiredConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return nil, err
	}
	settings, err := s.settingsLocked()
	if err != nil {
		return nil, err
	}
	return meshcfg.CompileDesired(&m, nodeID, meshcfg.DesiredDefaults{
		InterfaceName: settings.DefaultIface,
		PollInterval:  settings.DefaultPoll,
		Transport:     settings.TransportJSON,
	})
}

func nextAddress(subnet string, nodes []meshcfg.Node) (string, error) {
	if subnet == "" {
		subnet = "10.10.0.0/24"
	}
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil {
		return "", fmt.Errorf("vpnSubnet: %w", err)
	}
	if !prefix.Addr().Is4() {
		return "", fmt.Errorf("vpnSubnet must be IPv4")
	}
	base := prefix.Addr().As4()
	start := binary.BigEndian.Uint32(base[:])
	ones := prefix.Bits()
	hostBits := 32 - ones
	if hostBits < 2 {
		return "", fmt.Errorf("vpnSubnet too small")
	}
	maxHosts := (uint32(1) << hostBits) - 2
	used := make(map[uint32]struct{})
	for _, n := range nodes {
		host, err := hostIP4(n.Address)
		if err != nil {
			continue
		}
		used[host] = struct{}{}
	}
	for i := uint32(1); i <= maxHosts; i++ {
		ip := start + i
		if _, ok := used[ip]; ok {
			continue
		}
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], ip)
		addr := netip.AddrFrom4(b)
		return fmt.Sprintf("%s/%d", addr.String(), ones), nil
	}
	return "", fmt.Errorf("vpnSubnet %s exhausted", subnet)
}

func hostIP4(cidr string) (uint32, error) {
	cidr = strings.TrimSpace(cidr)
	if i := strings.IndexByte(cidr, '/'); i >= 0 {
		cidr = cidr[:i]
	}
	addr, err := netip.ParseAddr(cidr)
	if err != nil || !addr.Is4() {
		return 0, fmt.Errorf("bad addr")
	}
	b := addr.As4()
	return binary.BigEndian.Uint32(b[:]), nil
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
