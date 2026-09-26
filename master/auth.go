package master

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	password string
	ttl      time.Duration
}

func newSessionStore(password string) *sessionStore {
	return &sessionStore{
		sessions: make(map[string]time.Time),
		password: password,
		ttl:      24 * time.Hour,
	}
}

func (s *sessionStore) login(password string) (string, bool) {
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) != 1 {
		return "", false
	}
	var b [32]byte
	_, _ = rand.Read(b[:])
	tok := base64.RawURLEncoding.EncodeToString(b[:])
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return tok, true
}

func (s *sessionStore) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, tok)
		return false
	}
	return true
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
