package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"golang.zx2c4.com/wireguard/src/master/store"
)

var adminUserID = []byte("lasitan-admin")

type adminUser struct {
	creds []webauthn.Credential
}

func (u *adminUser) WebAuthnID() []byte                         { return adminUserID }
func (u *adminUser) WebAuthnName() string                        { return "admin" }
func (u *adminUser) WebAuthnDisplayName() string                 { return "Master 控制台" }
func (u *adminUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type waPending struct {
	data      webauthn.SessionData
	expiresAt time.Time
}

type waCeremonyStore struct {
	mu   sync.Mutex
	byID map[string]waPending
}

func newWACeremonyStore() *waCeremonyStore {
	return &waCeremonyStore{byID: make(map[string]waPending)}
}

func (c *waCeremonyStore) put(data webauthn.SessionData) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := base64.RawURLEncoding.EncodeToString(b[:])
	c.mu.Lock()
	c.purgeLocked()
	c.byID[id] = waPending{data: data, expiresAt: time.Now().Add(5 * time.Minute)}
	c.mu.Unlock()
	return id
}

func (c *waCeremonyStore) take(id string) (webauthn.SessionData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeLocked()
	p, ok := c.byID[id]
	if !ok {
		return webauthn.SessionData{}, false
	}
	delete(c.byID, id)
	if time.Now().After(p.expiresAt) {
		return webauthn.SessionData{}, false
	}
	return p.data, true
}

func (c *waCeremonyStore) purgeLocked() {
	now := time.Now()
	for k, v := range c.byID {
		if now.After(v.expiresAt) {
			delete(c.byID, k)
		}
	}
}

func (s *Server) loadAdminUser() (*adminUser, error) {
	list, err := s.store.ListWebAuthnCredentials()
	if err != nil {
		return nil, err
	}
	u := &adminUser{creds: make([]webauthn.Credential, 0, len(list))}
	for _, c := range list {
		u.creds = append(u.creds, toWACredential(c))
	}
	return u, nil
}

func toWACredential(c store.WebAuthnCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transport))
	for _, t := range c.Transport {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	flags := webauthn.CredentialFlags{
		UserPresent:    c.Flags&1 != 0,
		UserVerified:   c.Flags&2 != 0,
		BackupEligible: c.Flags&4 != 0,
		BackupState:    c.Flags&8 != 0,
	}
	return webauthn.Credential{
		ID:              c.ID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags:           flags,
		Authenticator: webauthn.Authenticator{
			AAGUID:       c.Authenticator.AAGUID,
			SignCount:    c.Authenticator.SignCount,
			CloneWarning: c.Authenticator.CloneWarning,
			Attachment:   protocol.AuthenticatorAttachment(c.Authenticator.Attachment),
		},
	}
}

func fromWACredential(cred *webauthn.Credential, name string, created time.Time) store.WebAuthnCredential {
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	var flags uint8
	if cred.Flags.UserPresent {
		flags |= 1
	}
	if cred.Flags.UserVerified {
		flags |= 2
	}
	if cred.Flags.BackupEligible {
		flags |= 4
	}
	if cred.Flags.BackupState {
		flags |= 8
	}
	if created.IsZero() {
		created = time.Now()
	}
	aaguid := cred.Authenticator.AAGUID
	if aaguid == nil {
		aaguid = []byte{}
	}
	return store.WebAuthnCredential{
		ID:              cred.ID,
		Name:            name,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		Transport:       transports,
		Flags:           flags,
		Authenticator: store.WebAuthnAuthenticator{
			AAGUID:       aaguid,
			SignCount:    cred.Authenticator.SignCount,
			CloneWarning: cred.Authenticator.CloneWarning,
			Attachment:   string(cred.Authenticator.Attachment),
		},
		CreatedAt: created,
	}
}

func (s *Server) webAuthnForRequest(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = strings.Split(h, ",")[0]
		host = strings.TrimSpace(host)
	}
	rpID := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		rpID = h
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		proto := "http"
		if r.TLS != nil {
			proto = "https"
		}
		if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
			proto = strings.TrimSpace(strings.Split(p, ",")[0])
		}
		origin = proto + "://" + host
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "Master 控制台",
		RPID:          rpID,
		RPOrigins:     []string{origin},
	})
}

func (s *Server) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authGate(w, r); !ok {
		return
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user, err := s.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	if len(user.creds) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no passkeys registered"})
		return
	}
	opts, session, err := wa.BeginLogin(user)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sid := s.waSessions.put(*session)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sid, "options": opts})
}

func (s *Server) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip, ok := s.authGate(w, r)
	if !ok {
		return
	}
	var body struct {
		SessionID  string          `json:"sessionId"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := readJSON(r, &body); err != nil || body.SessionID == "" || len(body.Credential) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	session, ok := s.waSessions.take(body.SessionID)
	if !ok {
		s.recordLoginFailure(w, ip, errWebAuthnFailed)
		return
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user, err := s.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body.Credential))
	if err != nil {
		s.recordLoginFailure(w, ip, errWebAuthnFailed)
		return
	}
	cred, err := wa.ValidateLogin(user, session, parsed)
	if err != nil {
		s.recordLoginFailure(w, ip, errWebAuthnFailed)
		return
	}
	_ = s.persistCredentialUpdate(cred)
	tok, err := s.sessions.issue()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.clearLoginFailures(ip)
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) persistCredentialUpdate(cred *webauthn.Credential) error {
	existing, found, err := s.store.GetWebAuthnCredential(cred.ID)
	if err != nil {
		return err
	}
	name := "通行密钥"
	created := time.Now()
	if found {
		name = existing.Name
		created = existing.CreatedAt
	}
	return s.store.PutWebAuthnCredential(fromWACredential(cred, name, created))
}

func (s *Server) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user, err := s.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	opts, session, err := wa.BeginRegistration(user)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sid := s.waSessions.put(*session)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sid, "options": opts})
}

func (s *Server) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		SessionID  string          `json:"sessionId"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := readJSON(r, &body); err != nil || body.SessionID == "" || len(body.Credential) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	session, ok := s.waSessions.take(body.SessionID)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or expired session"})
		return
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user, err := s.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body.Credential))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cred, err := wa.CreateCredential(user, session, parsed)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "通行密钥"
	}
	if err := s.store.PutWebAuthnCredential(fromWACredential(cred, name, time.Now())); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, credentialView(fromWACredential(cred, name, time.Now())))
}

type credentialViewOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

func credentialView(c store.WebAuthnCredential) credentialViewOut {
	return credentialViewOut{
		ID:        base64.RawURLEncoding.EncodeToString(c.ID),
		Name:      c.Name,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requireAdmin(w, r) {
			return
		}
		list, err := s.store.ListWebAuthnCredentials()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]credentialViewOut, 0, len(list))
		for _, c := range list {
			out = append(out, credentialView(c))
		}
		writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
	case http.MethodPatch:
		s.handleWebAuthnCredentialPatch(w, r)
	case http.MethodDelete:
		s.handleWebAuthnCredentialDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebAuthnAssertBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireAdmin(w, r) {
		return
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user, err := s.loadAdminUser()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth store error"})
		return
	}
	if len(user.creds) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no passkeys registered"})
		return
	}
	opts, session, err := wa.BeginLogin(user)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sid := s.waSessions.put(*session)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sid, "options": opts})
}

type stepUpBody struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Password             string          `json:"password"`
	WebAuthnSessionID    string          `json:"webauthnSessionId"`
	WebAuthnCredential   json.RawMessage `json:"webauthnCredential"`
}

func (s *Server) verifyStepUp(r *http.Request, body stepUpBody, excludeID []byte) error {
	if body.Password != "" {
		if s.sessions.checkPassword(body.Password) {
			return nil
		}
		return fmt.Errorf("invalid password")
	}
	if body.WebAuthnSessionID == "" || len(body.WebAuthnCredential) == 0 {
		return fmt.Errorf("password or passkey verification required")
	}
	session, ok := s.waSessions.take(body.WebAuthnSessionID)
	if !ok {
		return fmt.Errorf("invalid or expired webauthn session")
	}
	wa, err := s.webAuthnForRequest(r)
	if err != nil {
		return err
	}
	user, err := s.loadAdminUser()
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body.WebAuthnCredential))
	if err != nil {
		return fmt.Errorf("invalid assertion")
	}
	cred, err := wa.ValidateLogin(user, session, parsed)
	if err != nil {
		return fmt.Errorf("passkey verification failed")
	}
	if len(excludeID) > 0 && bytes.Equal(cred.ID, excludeID) {
		n, _ := s.store.CountWebAuthnCredentials()
		if n <= 1 {
			return fmt.Errorf("cannot verify deletion with the only passkey; use password")
		}
		return fmt.Errorf("use a different passkey or the password")
	}
	_ = s.persistCredentialUpdate(cred)
	return nil
}

func (s *Server) handleWebAuthnCredentialPatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body stepUpBody
	if err := readJSON(r, &body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	id, err := base64.RawURLEncoding.DecodeString(body.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := s.verifyStepUp(r, body, nil); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if err := s.store.RenameWebAuthnCredential(id, body.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	c, found, err := s.store.GetWebAuthnCredential(id)
	if err != nil || !found {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
		return
	}
	writeJSON(w, http.StatusOK, credentialView(c))
}

func (s *Server) handleWebAuthnCredentialDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body stepUpBody
	if err := readJSON(r, &body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	id, err := base64.RawURLEncoding.DecodeString(body.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := s.verifyStepUp(r, body, id); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	if err := s.store.DeleteWebAuthnCredential(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}