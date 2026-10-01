package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/src/master/store"
)

const sessionTTL = 24 * time.Hour

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	password string
	ttl      time.Duration
	store    *store.Store
}

func newSessionStore(password string, st *store.Store) *sessionStore {
	s := &sessionStore{
		sessions: make(map[string]time.Time),
		password: password,
		ttl:      sessionTTL,
		store:    st,
	}
	if st != nil {
		if list, err := st.ListAdminSessions(); err == nil {
			for _, sess := range list {
				s.sessions[sess.Token] = sess.ExpiresAt
			}
		}
	}
	return s
}

func (s *sessionStore) checkPassword(password string) bool {
	return subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
}

func (s *sessionStore) issue() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b[:])
	exp := time.Now().Add(s.ttl)
	if s.store != nil {
		if err := s.store.PutAdminSession(tok, exp); err != nil {
			return "", err
		}
	}
	s.mu.Lock()
	s.sessions[tok] = exp
	s.mu.Unlock()
	return tok, nil
}

func (s *sessionStore) login(password string) (string, bool) {
	if !s.checkPassword(password) {
		return "", false
	}
	tok, err := s.issue()
	if err != nil {
		return "", false
	}
	return tok, true
}

func (s *sessionStore) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	exp, ok := s.sessions[tok]
	s.mu.Unlock()
	if ok {
		if time.Now().After(exp) {
			s.revoke(tok)
			return false
		}
		return true
	}
	if s.store == nil {
		return false
	}
	exp, found, err := s.store.AdminSessionExpiry(tok)
	if err != nil || !found {
		return false
	}
	s.mu.Lock()
	s.sessions[tok] = exp
	s.mu.Unlock()
	return true
}

func (s *sessionStore) revoke(tok string) {
	s.mu.Lock()
	delete(s.sessions, tok)
	s.mu.Unlock()
	if s.store != nil {
		_ = s.store.DeleteAdminSession(tok)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(dst)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}
