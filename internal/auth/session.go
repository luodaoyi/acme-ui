package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CookieName = "acme_ui_session"

type Session struct {
	Token    string
	CSRF     string
	Created  time.Time
	LastSeen time.Time
}

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ttl      time.Duration
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

func GenerateToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func ConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *SessionStore) Create() (*Session, error) {
	token, err := GenerateToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := GenerateToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	session := &Session{
		Token:    token,
		CSRF:     csrf,
		Created:  now,
		LastSeen: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.sessions[token] = session
	return session, nil
}

func (s *SessionStore) FromRequest(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[cookie.Value]
	if !ok {
		return nil, false
	}
	if now.Sub(session.LastSeen) > s.ttl {
		delete(s.sessions, cookie.Value)
		return nil, false
	}
	session.LastSeen = now
	return session, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *SessionStore) pruneLocked(now time.Time) {
	for token, session := range s.sessions {
		if now.Sub(session.LastSeen) > s.ttl {
			delete(s.sessions, token)
		}
	}
}

type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	Failures     int
	BlockedUntil time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.attempts[ip]
	return time.Now().After(attempt.BlockedUntil)
}

func (l *LoginLimiter) Record(ip string, success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if success {
		delete(l.attempts, ip)
		return
	}
	attempt := l.attempts[ip]
	attempt.Failures++
	if attempt.Failures >= 5 {
		attempt.BlockedUntil = time.Now().Add(30 * time.Second)
	}
	l.attempts[ip] = attempt
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
