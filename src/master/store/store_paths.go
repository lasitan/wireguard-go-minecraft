package store

import (
	"bytes"
	"encoding/json"

	"golang.zx2c4.com/wireguard/src/core"
)

const metaPaths = "paths_json"

func (s *Store) pathsLocked() core.PathChoices {
	raw, err := s.metaGet(metaPaths)
	if err != nil || raw == "" {
		return nil
	}
	var p core.PathChoices
	if json.Unmarshal([]byte(raw), &p) != nil {
		return nil
	}
	return p
}

// SetPaths stores the gateway choices and bumps the revision when they changed,
// so affected agents get re-pushed. Reports whether anything changed.
func (s *Store) SetPaths(p core.PathChoices) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	if len(p) == 0 {
		next = []byte("{}")
	}
	prev, _ := s.metaGet(metaPaths)
	if prev == "" {
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
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaPaths, string(next)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = ?`, metaRevision); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
