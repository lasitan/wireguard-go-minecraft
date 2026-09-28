package store

// DefaultTransportJSON is the mesh-wide TCP camouflage config for new Master databases.
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

func (s *Store) ensureTransportDefault() error {
	tr, err := s.metaGet(metaTransportJSON)
	if err != nil {
		return err
	}
	if len(tr) > 0 {
		return nil
	}
	_, err = s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTransportJSON, DefaultTransportJSON)
	return err
}
