package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	authFailWindow = 30 * time.Minute
	authFailLimit  = 5
)

// AdminSession is a persisted UI bearer token.
type AdminSession struct {
	Token     string
	ExpiresAt time.Time
}

// WebAuthnCredential is a stored passkey for the single admin user.
type WebAuthnCredential struct {
	ID              []byte
	Name            string
	PublicKey       []byte
	AttestationType string
	Transport       []string
	Flags           uint8
	Authenticator   WebAuthnAuthenticator
	CreatedAt       time.Time
}

// WebAuthnAuthenticator holds sign-count / AAGUID fields from go-webauthn.
type WebAuthnAuthenticator struct {
	AAGUID       []byte
	SignCount    uint32
	CloneWarning bool
	Attachment   string
}

func (s *Store) migrateAuth() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS auth_bans (
  ip TEXT PRIMARY KEY NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS auth_failures (
  ip TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_failures_ip_at ON auth_failures(ip, at);
CREATE TABLE IF NOT EXISTS admin_sessions (
  token TEXT PRIMARY KEY NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS webauthn_credentials (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  public_key BLOB NOT NULL,
  attestation_type TEXT NOT NULL DEFAULT '',
  transport_json TEXT NOT NULL DEFAULT '[]',
  flags INTEGER NOT NULL DEFAULT 0,
  aaguid BLOB NOT NULL DEFAULT X'',
  sign_count INTEGER NOT NULL DEFAULT 0,
  clone_warning INTEGER NOT NULL DEFAULT 0,
  attachment TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
`)
	return err
}

// IsAuthBanned reports whether ip is permanently banned from login.
func (s *Store) IsAuthBanned(ip string) (bool, error) {
	if ip == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM auth_bans WHERE ip = ?`, ip).Scan(&n)
	return n > 0, err
}

// BanAuthIP permanently bans ip from login attempts.
// Manual DB edit is the only unban path (no self-service API).
func (s *Store) BanAuthIP(ip string) error {
	if ip == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO auth_bans(ip, created_at) VALUES(?, ?)`,
		ip, time.Now().Unix(),
	)
	return err
}

// RecordAuthFailure appends a failure and returns (bannedNow, totalInWindow, err).
// When the rolling 30-minute failure count reaches 5, the IP is permanently banned.
func (s *Store) RecordAuthFailure(ip string) (banned bool, count int, err error) {
	if ip == "" {
		return false, 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-authFailWindow).Unix()
	if _, err = s.db.Exec(`DELETE FROM auth_failures WHERE at < ?`, cutoff); err != nil {
		return false, 0, err
	}
	if _, err = s.db.Exec(`INSERT INTO auth_failures(ip, at) VALUES(?, ?)`, ip, now.Unix()); err != nil {
		return false, 0, err
	}
	if err = s.db.QueryRow(
		`SELECT COUNT(1) FROM auth_failures WHERE ip = ? AND at >= ?`, ip, cutoff,
	).Scan(&count); err != nil {
		return false, 0, err
	}
	if count >= authFailLimit {
		if _, err = s.db.Exec(
			`INSERT OR IGNORE INTO auth_bans(ip, created_at) VALUES(?, ?)`,
			ip, now.Unix(),
		); err != nil {
			return false, count, err
		}
		_, _ = s.db.Exec(`DELETE FROM auth_failures WHERE ip = ?`, ip)
		return true, count, nil
	}
	return false, count, nil
}

// ClearAuthFailures removes recent failures for ip (call on successful login).
func (s *Store) ClearAuthFailures(ip string) error {
	if ip == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM auth_failures WHERE ip = ?`, ip)
	return err
}

// PutAdminSession persists a bearer token until expiresAt.
func (s *Store) PutAdminSession(token string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO admin_sessions(token, expires_at) VALUES(?, ?)`,
		token, expiresAt.Unix(),
	)
	return err
}

// DeleteAdminSession removes a bearer token.
func (s *Store) DeleteAdminSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM admin_sessions WHERE token = ?`, token)
	return err
}

// AdminSessionExpiry returns expiry for token, or false if missing/expired (and deletes expired).
func (s *Store) AdminSessionExpiry(token string) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exp int64
	err := s.db.QueryRow(`SELECT expires_at FROM admin_sessions WHERE token = ?`, token).Scan(&exp)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t := time.Unix(exp, 0)
	if time.Now().After(t) {
		_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE token = ?`, token)
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// ListAdminSessions returns non-expired sessions (also purges expired rows).
func (s *Store) ListAdminSessions() ([]AdminSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	if _, err := s.db.Exec(`DELETE FROM admin_sessions WHERE expires_at < ?`, now); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT token, expires_at FROM admin_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminSession
	for rows.Next() {
		var tok string
		var exp int64
		if err := rows.Scan(&tok, &exp); err != nil {
			return nil, err
		}
		out = append(out, AdminSession{Token: tok, ExpiresAt: time.Unix(exp, 0)})
	}
	return out, rows.Err()
}

func credIDKey(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

// ListWebAuthnCredentials returns all passkeys (newest first).
func (s *Store) ListWebAuthnCredentials() ([]WebAuthnCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT id, name, public_key, attestation_type, transport_json, flags,
       aaguid, sign_count, clone_warning, attachment, created_at
FROM webauthn_credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebAuthnCredential
	for rows.Next() {
		c, err := scanWebAuthnCred(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanWebAuthnCred(row scannable) (WebAuthnCredential, error) {
	var (
		idKey, name, attType, transportJSON, attachment string
		pub                                             []byte
		flags                                           int
		aaguid                                          []byte
		signCount, cloneWarn, created                   int64
	)
	if err := row.Scan(&idKey, &name, &pub, &attType, &transportJSON, &flags,
		&aaguid, &signCount, &cloneWarn, &attachment, &created); err != nil {
		return WebAuthnCredential{}, err
	}
	id, err := base64.RawURLEncoding.DecodeString(idKey)
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("credential id: %w", err)
	}
	var transport []string
	if transportJSON != "" {
		_ = json.Unmarshal([]byte(transportJSON), &transport)
	}
	return WebAuthnCredential{
		ID:              id,
		Name:            name,
		PublicKey:       pub,
		AttestationType: attType,
		Transport:       transport,
		Flags:           uint8(flags),
		Authenticator: WebAuthnAuthenticator{
			AAGUID:       aaguid,
			SignCount:    uint32(signCount),
			CloneWarning: cloneWarn != 0,
			Attachment:   attachment,
		},
		CreatedAt: time.Unix(created, 0),
	}, nil
}

// GetWebAuthnCredential loads one passkey by credential ID bytes.
func (s *Store) GetWebAuthnCredential(id []byte) (WebAuthnCredential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`
SELECT id, name, public_key, attestation_type, transport_json, flags,
       aaguid, sign_count, clone_warning, attachment, created_at
FROM webauthn_credentials WHERE id = ?`, credIDKey(id))
	c, err := scanWebAuthnCred(row)
	if err == sql.ErrNoRows {
		return WebAuthnCredential{}, false, nil
	}
	if err != nil {
		return WebAuthnCredential{}, false, err
	}
	return c, true, nil
}

// PutWebAuthnCredential inserts or updates a passkey.
func (s *Store) PutWebAuthnCredential(c WebAuthnCredential) error {
	if len(c.ID) == 0 {
		return fmt.Errorf("empty credential id")
	}
	if c.Name == "" {
		c.Name = "通行密钥"
	}
	if c.PublicKey == nil {
		c.PublicKey = []byte{}
	}
	if c.Authenticator.AAGUID == nil {
		c.Authenticator.AAGUID = []byte{}
	}
	tj, _ := json.Marshal(c.Transport)
	clone := 0
	if c.Authenticator.CloneWarning {
		clone = 1
	}
	created := c.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
INSERT INTO webauthn_credentials(
  id, name, public_key, attestation_type, transport_json, flags,
  aaguid, sign_count, clone_warning, attachment, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  public_key=excluded.public_key,
  attestation_type=excluded.attestation_type,
  transport_json=excluded.transport_json,
  flags=excluded.flags,
  aaguid=excluded.aaguid,
  sign_count=excluded.sign_count,
  clone_warning=excluded.clone_warning,
  attachment=excluded.attachment
`, credIDKey(c.ID), c.Name, c.PublicKey, c.AttestationType, string(tj), int(c.Flags),
		c.Authenticator.AAGUID, int64(c.Authenticator.SignCount), clone, c.Authenticator.Attachment, created.Unix())
	return err
}

// RenameWebAuthnCredential sets the display name.
func (s *Store) RenameWebAuthnCredential(id []byte, name string) error {
	name = trimCredentialName(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE webauthn_credentials SET name = ? WHERE id = ?`, name, credIDKey(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// DeleteWebAuthnCredential removes a passkey by id.
func (s *Store) DeleteWebAuthnCredential(id []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM webauthn_credentials WHERE id = ?`, credIDKey(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// CountWebAuthnCredentials returns how many passkeys are registered.
func (s *Store) CountWebAuthnCredentials() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM webauthn_credentials`).Scan(&n)
	return n, err
}

func trimCredentialName(name string) string {
	n := strings.TrimSpace(name)
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// AuthFailLimit is the max failures within AuthFailWindow before permanent ban.
func AuthFailLimit() int { return authFailLimit }

// AuthFailWindow is the rolling window for login failures.
func AuthFailWindow() time.Duration { return authFailWindow }
