package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	SessionCookieName = "unifi_switch_session"
	CSRFHeaderName    = "X-CSRF-Token"
	SessionTTL        = 24 * time.Hour
	MaxLoginAttempts  = 5
	LockoutWindow     = 1 * time.Minute
)

type Session struct {
	ID        string
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

type loginAttempt struct {
	count       int
	firstFailed time.Time
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	attempts map[string]*loginAttempt
}

func NewManager() *Manager {
	m := &Manager{
		sessions: make(map[string]*Session),
		attempts: make(map[string]*loginAttempt),
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for id, s := range m.sessions {
			if now.After(s.ExpiresAt) {
				delete(m.sessions, id)
			}
		}
		for ip, a := range m.attempts {
			if now.Sub(a.firstFailed) > LockoutWindow {
				delete(m.attempts, ip)
			}
		}
		m.mu.Unlock()
	}
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (m *Manager) IsRateLimited(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.attempts[ip]
	if !ok {
		return false
	}
	if time.Since(a.firstFailed) > LockoutWindow {
		delete(m.attempts, ip)
		return false
	}
	return a.count >= MaxLoginAttempts
}

func (m *Manager) RecordFailure(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.attempts[ip]
	if !ok || time.Since(a.firstFailed) > LockoutWindow {
		m.attempts[ip] = &loginAttempt{count: 1, firstFailed: time.Now()}
		return
	}
	a.count++
}

func (m *Manager) ResetFailures(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attempts, ip)
}

func (m *Manager) CreateSession(username string) (*Session, error) {
	sid, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	s := &Session{
		ID:        sid,
		Username:  username,
		CSRFToken: csrf,
		ExpiresAt: time.Now().Add(SessionTTL),
	}
	m.mu.Lock()
	m.sessions[sid] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) GetSession(r *http.Request) *Session {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	m.mu.RLock()
	s, ok := m.sessions[c.Value]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		m.DestroySession(c.Value)
		return nil
	}
	return s
}

func (m *Manager) DestroySession(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) DestroyAllSessions() {
	m.mu.Lock()
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
}

func SetSessionCookie(w http.ResponseWriter, r *http.Request, s *Session) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    s.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(SessionTTL.Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ValidateCSRFAndOrigin verifies both Same-Origin (Origin/Referer headers) and X-CSRF-Token
// on state-changing requests (POST, PUT, PATCH, DELETE).
func ValidateCSRFAndOrigin(r *http.Request, s *Session) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	reqHost := r.Host
	fwdHost := r.Header.Get("X-Forwarded-Host")
	// Check Origin if present
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (!sameHost(u.Host, reqHost) && (fwdHost == "" || !sameHost(u.Host, fwdHost))) {
			return false
		}
	} else if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		if err != nil || (!sameHost(u.Host, reqHost) && (fwdHost == "" || !sameHost(u.Host, fwdHost))) {
			return false
		}
	}

	if s == nil {
		return false
	}
	token := r.Header.Get(CSRFHeaderName)
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRFToken)) == 1
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
