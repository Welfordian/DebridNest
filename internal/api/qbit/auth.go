package qbit

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "SID"

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

func (s *sessionStore) create() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[sid] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return sid, nil
}

func (s *sessionStore) valid(sid string) bool {
	if sid == "" {
		return false
	}
	s.mu.RLock()
	expires, ok := s.sessions[sid]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		s.mu.Lock()
		delete(s.sessions, sid)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) delete(sid string) {
	if sid == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()
}

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err == nil && h.sessions.valid(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if user, password, ok := r.BasicAuth(); ok && h.authenticateLogin(r.Context(), user, password) {
			next.ServeHTTP(w, r)
			return
		}
		if h.auth != nil {
			if _, ok := h.auth.ValidateToken(r.Context(), r.Header.Get("Authorization")); ok {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.WriteHeader(http.StatusForbidden)
	})
}

func setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
