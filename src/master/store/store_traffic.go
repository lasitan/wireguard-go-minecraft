package master

import (
	"database/sql"
	"time"

	"golang.zx2c4.com/wireguard/meshcfg"
)

const (
	minuteRetention = 7 * 24 * time.Hour
	hourRetention   = 90 * 24 * time.Hour
)

func (s *Store) migrateTraffic() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS traffic_totals (
  node_id TEXT PRIMARY KEY NOT NULL,
  rx INTEGER NOT NULL DEFAULT 0,
  tx INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS traffic_minute (
  node_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx INTEGER NOT NULL DEFAULT 0,
  tx INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS traffic_hour (
  node_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx INTEGER NOT NULL DEFAULT 0,
  tx INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS traffic_peer_ip (
  node_id TEXT NOT NULL,
  ip TEXT NOT NULL,
  rx INTEGER NOT NULL DEFAULT 0,
  tx INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, ip)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS geo_cache (
  ip TEXT PRIMARY KEY NOT NULL,
  country TEXT NOT NULL DEFAULT '',
  country_code TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}

// TrafficDelta is the accumulated byte delta for one node since the last flush.
type TrafficDelta struct {
	Rx, Tx uint64
	// Minute buckets (unix seconds aligned to minute) -> delta.
	Minutes map[int64][2]uint64
	// Per VPN IP delta.
	IPs map[string][2]uint64
}

func (d *TrafficDelta) Empty() bool {
	return d.Rx == 0 && d.Tx == 0 && len(d.IPs) == 0
}

// FlushTraffic writes all pending deltas in one transaction (single writer).
func (s *Store) FlushTraffic(batch map[string]*TrafficDelta, now time.Time) error {
	if len(batch) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now.Unix()
	for nodeID, d := range batch {
		if d.Rx != 0 || d.Tx != 0 {
			if _, err := tx.Exec(`INSERT INTO traffic_totals(node_id, rx, tx, updated_at) VALUES(?,?,?,?)
				ON CONFLICT(node_id) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx, updated_at = excluded.updated_at`,
				nodeID, int64(d.Rx), int64(d.Tx), ts); err != nil {
				return err
			}
		}
		for m, v := range d.Minutes {
			if _, err := tx.Exec(`INSERT INTO traffic_minute(node_id, ts, rx, tx) VALUES(?,?,?,?)
				ON CONFLICT(node_id, ts) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx`,
				nodeID, m, int64(v[0]), int64(v[1])); err != nil {
				return err
			}
		}
		for ip, v := range d.IPs {
			if _, err := tx.Exec(`INSERT INTO traffic_peer_ip(node_id, ip, rx, tx, updated_at) VALUES(?,?,?,?,?)
				ON CONFLICT(node_id, ip) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx, updated_at = excluded.updated_at`,
				nodeID, ip, int64(v[0]), int64(v[1]), ts); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// RollupAndPrune folds complete minute buckets into hour buckets and deletes
// data past retention. Safe to call repeatedly (hour buckets are recomputed).
func (s *Store) RollupAndPrune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	curHour := now.Truncate(time.Hour).Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Recompute hour buckets touched in the last 2 hours plus any older minute data.
	if _, err := tx.Exec(`INSERT INTO traffic_hour(node_id, ts, rx, tx)
		SELECT node_id, (ts / 3600) * 3600 AS h, SUM(rx), SUM(tx) FROM traffic_minute
		WHERE ts < ? GROUP BY node_id, h
		ON CONFLICT(node_id, ts) DO UPDATE SET rx = excluded.rx, tx = excluded.tx`, curHour); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traffic_minute WHERE ts < ?`, now.Add(-minuteRetention).Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM traffic_hour WHERE ts < ?`, now.Add(-hourRetention).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

type TrafficPoint struct {
	TS int64  `json:"ts"`
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

// TrafficSeries returns buckets from the minute or hour table in [since, now].
func (s *Store) TrafficSeries(nodeID string, since time.Time, hourly bool) ([]TrafficPoint, error) {
	table := "traffic_minute"
	if hourly {
		table = "traffic_hour"
	}
	rows, err := s.rdb.Query(`SELECT ts, rx, tx FROM `+table+` WHERE node_id = ? AND ts >= ? ORDER BY ts`, nodeID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrafficPoint{}
	for rows.Next() {
		var p TrafficPoint
		var rx, tx int64
		if err := rows.Scan(&p.TS, &rx, &tx); err != nil {
			return nil, err
		}
		p.Rx, p.Tx = uint64(rx), uint64(tx)
		out = append(out, p)
	}
	return out, rows.Err()
}

type TrafficTotals struct {
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

func (s *Store) TrafficTotal(nodeID string) (TrafficTotals, error) {
	var rx, tx int64
	err := s.rdb.QueryRow(`SELECT rx, tx FROM traffic_totals WHERE node_id = ?`, nodeID).Scan(&rx, &tx)
	if err == sql.ErrNoRows {
		return TrafficTotals{}, nil
	}
	return TrafficTotals{Rx: uint64(rx), Tx: uint64(tx)}, err
}

type PeerIPTraffic struct {
	IP        string `json:"ip"`
	Rx        uint64 `json:"rx"`
	Tx        uint64 `json:"tx"`
	UpdatedAt int64  `json:"updatedAt"`
}

func (s *Store) PeerIPTraffic(nodeID string) ([]PeerIPTraffic, error) {
	rows, err := s.rdb.Query(`SELECT ip, rx, tx, updated_at FROM traffic_peer_ip WHERE node_id = ? ORDER BY (rx + tx) DESC LIMIT 1000`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PeerIPTraffic{}
	for rows.Next() {
		var p PeerIPTraffic
		var rx, tx int64
		if err := rows.Scan(&p.IP, &rx, &tx, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Rx, p.Tx = uint64(rx), uint64(tx)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) deleteTrafficLocked(nodeID string) error {
	for _, t := range []string{"traffic_totals", "traffic_minute", "traffic_hour", "traffic_peer_ip"} {
		if _, err := s.db.Exec(`DELETE FROM `+t+` WHERE node_id = ?`, nodeID); err != nil {
			return err
		}
	}
	return nil
}

// ---- lightweight node updates (no revision bump) ----

// NodeByToken resolves an agent token without loading the whole mesh.
func (s *Store) NodeByToken(token string) (meshcfg.Node, error) {
	var n meshcfg.Node
	var enabled int
	err := s.rdb.QueryRow(`SELECT id, name, address, public_v4, public_v6, enabled FROM nodes WHERE token = ?`, token).
		Scan(&n.ID, &n.Name, &n.Address, &n.PublicV4, &n.PublicV6, &enabled)
	if err == sql.ErrNoRows {
		return n, errUnauthorized
	}
	n.Disabled = enabled == 0
	n.Token = token
	return n, err
}

func (s *Store) TouchLastSeen(nodeID string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE nodes SET last_seen = ? WHERE id = ?`, formatTime(t), nodeID)
	return err
}

func (s *Store) SetPublicIPs(nodeID, v4, v6 string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE nodes SET public_v4 = ?, public_v6 = ? WHERE id = ?`, v4, v6, nodeID)
	return err
}

func (s *Store) SetNodeGeo(nodeID, country, code string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE nodes SET geo_country = ?, geo_country_code = ?, geo_updated_at = ? WHERE id = ?`,
		country, code, formatTime(t), nodeID)
	return err
}

func (s *Store) GeoCacheGet(ip string, maxAge time.Duration) (country, code string, ok bool) {
	var ts int64
	err := s.rdb.QueryRow(`SELECT country, country_code, updated_at FROM geo_cache WHERE ip = ?`, ip).Scan(&country, &code, &ts)
	if err != nil || time.Since(time.Unix(ts, 0)) > maxAge {
		return "", "", false
	}
	return country, code, true
}

func (s *Store) GeoCachePut(ip, country, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO geo_cache(ip, country, country_code, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(ip) DO UPDATE SET country = excluded.country, country_code = excluded.country_code, updated_at = excluded.updated_at`,
		ip, country, code, time.Now().Unix())
	return err
}

func (s *Store) Close() error {
	if s.rdb != nil {
		_ = s.rdb.Close()
	}
	return s.db.Close()
}
