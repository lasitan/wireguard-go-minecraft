package store

import "strings"

// DefaultTransportJSON is the mesh-wide TCP camouflage config for new Master
// databases. The secret placeholder is replaced with a random value per Master.
const DefaultTransportJSON = `{
  "tcp": {
    "dialTimeout": "3s",
    "reconnectInitialBackoff": "1s",
    "reconnectMaxBackoff": "60s",
    "rxIdleTimeout": "5s"
  },
  "camouflage": {
    "profile": "minecraft",
    "deep": true,
    "secure": true,
    "handshakeTimeout": "10s",
    "loginUsername": "Steve",
    "loginPluginChannel": "minecraft:register",
    "loginPluginSecret": "change-me-shared-secret",
    "rejectMessage": "You are not whitelisted on this server!",
    "serverName": "Dedicated Server"
  },
  "mc": {
    "enabled": true,
    "handshakeTimeout": "10s",
    "deepCamouflage": true,
    "loginUsername": "Steve",
    "loginPluginChannel": "minecraft:register",
    "loginPluginSecret": "change-me-shared-secret"
  }
}`

// placeholderSecret was shipped as the default camouflage secret; anyone who
// has read the source knows it, so it is never left in place.
const placeholderSecret = "change-me-shared-secret"

func (s *Store) ensureTransportDefault() error {
	tr, err := s.metaGet(metaTransportJSON)
	if err != nil {
		return err
	}
	if len(tr) > 0 && !strings.Contains(tr, `"`+placeholderSecret+`"`) {
		return nil
	}
	if len(tr) == 0 {
		tr = DefaultTransportJSON
	}
	secret, err := NewEnrollToken()
	if err != nil {
		return err
	}
	tr = strings.ReplaceAll(tr, `"`+placeholderSecret+`"`, `"`+secret+`"`)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTransportJSON, tr); err != nil {
		return err
	}
	// Agents only re-apply on a new revision.
	if _, err := tx.Exec(`UPDATE meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = ?`, metaRevision); err != nil {
		return err
	}
	return tx.Commit()
}
