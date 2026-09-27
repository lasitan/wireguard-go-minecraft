package store

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

	"golang.zx2c4.com/wireguard/src/core"

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
	db   *sql.DB // single writer connection
	rdb  *sql.DB // read-only pool (WAL allows concurrent readers)
	path string
}

type Settings struct {
	EnrollToken   string
	VPNSubnet     string
	DefaultIface  string
	DefaultPoll   string
	TransportJSON json.RawMessage
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "mesh.db")
	pragmas := "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+pragmas)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.ensureSeeds(); err != nil {
		_ = db.Close()
		return nil, err
	}
	rdb, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+pragmas+"&_pragma=query_only(1)")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	rdb.SetMaxOpenConns(4)
	s.rdb = rdb
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
	if err != nil {
		return err
	}
	if err := s.addMissingNodeColumns(); err != nil {
		return err
	}
	return s.migrateTraffic()
}

// addMissingNodeColumns upgrades databases created before these fields existed.
func (s *Store) addMissingNodeColumns() error {
	have := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(nodes)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	cols := []struct{ name, ddl string }{
		{"enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"routes_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"address_changed_at", "TEXT NOT NULL DEFAULT ''"},
		{"public_v4", "TEXT NOT NULL DEFAULT ''"},
		{"public_v6", "TEXT NOT NULL DEFAULT ''"},
		{"geo_country", "TEXT NOT NULL DEFAULT ''"},
		{"geo_country_code", "TEXT NOT NULL DEFAULT ''"},
		{"geo_updated_at", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range cols {
		if have[c.name] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE nodes ADD COLUMN ` + c.name + ` ` + c.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", c.name, err)
		}
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// ensureSeeds fills first-run defaults. Enroll token and address pool are
// edited from the web UI afterwards; existing values are never overwritten.
func (s *Store) ensureSeeds() error {
	enroll, err := NewEnrollToken()
	if err != nil {
		return err
	}
	defaults := map[string]string{
		metaRevision:     "0",
		metaEnrollToken:  enroll,
		metaVPNSubnet:    DefaultVPNSubnet,
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
	var m core.Mesh
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

func (s *Store) Settings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settingsLocked()
}

func (s *Store) settingsLocked() (Settings, error) {
	out := Settings{}
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

func (s *Store) Snapshot() core.Mesh {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return core.EmptyMesh()
	}
	return m
}

func (s *Store) loadMeshLocked() (core.Mesh, error) {
	m := core.EmptyMesh()
	revStr, err := s.metaGet(metaRevision)
	if err != nil {
		return m, err
	}
	fmt.Sscanf(revStr, "%d", &m.Revision)

	rows, err := s.db.Query(`SELECT id, name, role, public_key, private_key, address, listen_port, endpoint, mtu, token, iface_name, poll_interval, transport_json, last_seen,
		enabled, routes_json, address_changed_at, public_v4, public_v6, geo_country, geo_country_code, geo_updated_at FROM nodes ORDER BY rowid`)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var n core.Node
		var listenPort, mtu, enabled int
		var transport, lastSeen, routes, addrChanged, geoUpdated string
		if err := rows.Scan(&n.ID, &n.Name, &n.Role, &n.PublicKey, &n.PrivateKey, &n.Address, &listenPort, &n.Endpoint, &mtu, &n.Token, &n.Interface, &n.PollInterval, &transport, &lastSeen,
			&enabled, &routes, &addrChanged, &n.PublicV4, &n.PublicV6, &n.GeoCountry, &n.GeoCountryCode, &geoUpdated); err != nil {
			return m, err
		}
		n.ListenPort = uint16(listenPort)
		n.MTU = mtu
		if transport != "" {
			n.Transport = json.RawMessage(transport)
		}
		n.LastSeen = parseTime(lastSeen)
		n.Disabled = enabled == 0
		_ = json.Unmarshal([]byte(routes), &n.Routes)
		n.AddressChangedAt = parseTime(addrChanged)
		n.GeoUpdatedAt = parseTime(geoUpdated)
		m.Nodes = append(m.Nodes, n)
	}

	lrows, err := s.db.Query(`SELECT from_node_id, to_node_id, allowed_ips, keepalive FROM links ORDER BY rowid`)
	if err != nil {
		return m, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l core.Link
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
		var f core.Forward
		var port int
		if err := frows.Scan(&f.NodeID, &f.Protocol, &f.Listen, &f.DestNodeID, &port); err != nil {
			return m, err
		}
		f.DestPort = uint16(port)
		m.Forwards = append(m.Forwards, f)
	}
	return m, nil
}

func (s *Store) PutMesh(m core.Mesh) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.loadMeshLocked()
	if err != nil {
		return err
	}
	existing := make(map[string]core.Node, len(cur.Nodes))
	for _, n := range cur.Nodes {
		existing[n.ID] = n
	}
	for i := range m.Nodes {
		c, ok := existing[m.Nodes[i].ID]
		if !ok {
			return fmt.Errorf("cannot add node %q via mesh edit; agents must enroll", m.Nodes[i].ID)
		}
		m.Nodes[i].PreserveServerFields(c)
	}
	m.Revision = cur.Revision + 1
	if err := validateMesh(m); err != nil {
		return err
	}
	return s.replaceMeshLocked(m)
}

func (s *Store) replaceMeshLocked(m core.Mesh) error {
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
		routes, _ := json.Marshal(n.Routes)
		if n.Routes == nil {
			routes = []byte("[]")
		}
		enabled := 1
		if n.Disabled {
			enabled = 0
		}
		if _, err := tx.Exec(`INSERT INTO nodes(id, name, role, public_key, private_key, address, listen_port, endpoint, mtu, token, iface_name, poll_interval, transport_json, last_seen,
			enabled, routes_json, address_changed_at, public_v4, public_v6, geo_country, geo_country_code, geo_updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			n.ID, n.Name, n.Role, n.PublicKey, n.PrivateKey, n.Address, int(n.ListenPort), n.Endpoint, n.MTU, n.Token, n.Interface, n.PollInterval, tr, formatTime(n.LastSeen),
			enabled, string(routes), formatTime(n.AddressChangedAt), n.PublicV4, n.PublicV6, n.GeoCountry, n.GeoCountryCode, formatTime(n.GeoUpdatedAt)); err != nil {
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

func (s *Store) Enroll(name, role, endpoint string, listenPort uint16) (core.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if role == "" {
		role = core.RoleClient
	}
	if role != core.RoleServer && role != core.RoleClient {
		return core.Node{}, fmt.Errorf("role must be server or client")
	}
	settings, err := s.settingsLocked()
	if err != nil {
		return core.Node{}, err
	}
	m, err := s.loadMeshLocked()
	if err != nil {
		return core.Node{}, err
	}
	id, err := core.GenerateNodeID()
	if err != nil {
		return core.Node{}, err
	}
	priv, pub, err := core.GenerateKeyPair()
	if err != nil {
		return core.Node{}, err
	}
	tok, err := core.GenerateToken()
	if err != nil {
		return core.Node{}, err
	}
	addr, err := nextAddress(settings.VPNSubnet, m.Nodes)
	if err != nil {
		return core.Node{}, err
	}
	n := core.Node{
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

		AddressChangedAt: time.Now().UTC(),
	}
	if role == core.RoleServer && n.ListenPort == 0 {
		n.ListenPort = DefaultServerListenPort
	}
	m.Nodes = append(m.Nodes, n)
	// Auto-attach: a new card joins the first mother; a new mother adopts
	// every card that is not attached anywhere yet.
	if n.IsMagnetParent() {
		attached := map[string]bool{}
		for _, l := range m.Links {
			attached[l.FromNodeID] = true
		}
		for _, peer := range m.Nodes {
			if peer.ID == n.ID || peer.IsMagnetParent() || attached[peer.ID] {
				continue
			}
			m.Links = append(m.Links, core.Link{FromNodeID: peer.ID, ToNodeID: n.ID, Keepalive: magnetKeepalive})
		}
	} else {
		for _, peer := range m.Nodes {
			if peer.ID != n.ID && peer.IsMagnetParent() {
				m.Links = append(m.Links, core.Link{FromNodeID: n.ID, ToNodeID: peer.ID, Keepalive: magnetKeepalive})
				break
			}
		}
	}
	m.Revision++
	if err := validateMesh(m); err != nil {
		return core.Node{}, err
	}
	if err := s.replaceMeshLocked(m); err != nil {
		return core.Node{}, err
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
	var links []core.Link
	for _, l := range m.Links {
		if l.FromNodeID == id || l.ToNodeID == id {
			continue
		}
		links = append(links, l)
	}
	m.Links = links
	var forwards []core.Forward
	for _, f := range m.Forwards {
		if f.NodeID == id || f.DestNodeID == id {
			continue
		}
		forwards = append(forwards, f)
	}
	m.Forwards = forwards
	m.Revision++
	if err := s.replaceMeshLocked(m); err != nil {
		return err
	}
	return s.deleteTrafficLocked(id)
}

func (s *Store) DesiredForToken(token string) (*core.DesiredConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.loadMeshLocked()
	if err != nil {
		return nil, err
	}
	n := m.FindNodeByToken(token)
	if n == nil {
		return nil, ErrUnauthorized
	}
	n.LastSeen = time.Now().UTC()
	_, _ = s.db.Exec(`UPDATE nodes SET last_seen = ? WHERE id = ?`, n.LastSeen.UTC().Format(time.RFC3339Nano), n.ID)
	settings, err := s.settingsLocked()
	if err != nil {
		return nil, err
	}
	return core.CompileDesired(&m, n.ID, core.DesiredDefaults{
		InterfaceName: settings.DefaultIface,
		PollInterval:  settings.DefaultPoll,
		Transport:     settings.TransportJSON,
	})
}

func (s *Store) DesiredForNode(nodeID string) (*core.DesiredConfig, error) {
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
	return core.CompileDesired(&m, nodeID, core.DesiredDefaults{
		InterfaceName: settings.DefaultIface,
		PollInterval:  settings.DefaultPoll,
		Transport:     settings.TransportJSON,
	})
}

// DesiredForNodes compiles configs for many nodes from a single mesh load.
// Unknown ids are skipped.
func (s *Store) DesiredForNodes(ids []string) (map[string]*core.DesiredConfig, error) {
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
	def := core.DesiredDefaults{
		InterfaceName: settings.DefaultIface,
		PollInterval:  settings.DefaultPoll,
		Transport:     settings.TransportJSON,
	}
	out := make(map[string]*core.DesiredConfig, len(ids))
	for _, id := range ids {
		if m.FindNode(id) == nil {
			continue
		}
		d, err := core.CompileDesired(&m, id, def)
		if err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, nil
}

func nextAddress(subnet string, nodes []core.Node) (string, error) {
	if subnet == "" {
		subnet = DefaultVPNSubnet
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

var ErrUnauthorized = fmt.Errorf("unauthorized")

func validateMesh(m core.Mesh) error {
	ids := make(map[string]struct{}, len(m.Nodes))
	for _, n := range m.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node missing id")
		}
		if !core.ValidNodeID(n.ID) {
			return fmt.Errorf("node id %q must be a UUID", n.ID)
		}
		if _, ok := ids[n.ID]; ok {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = struct{}{}
		if n.Role != core.RoleServer && n.Role != core.RoleClient {
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
