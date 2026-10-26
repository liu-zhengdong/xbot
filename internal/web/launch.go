package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
)

func (s *Server) launchTime() time.Time {
	if s.launchNow != nil {
		return s.launchNow()
	}
	return time.Now()
}

// NewLaunchCode creates a single-use login code valid for sixty seconds.
// Codes live only in memory and must never be logged.
func (s *Server) NewLaunchCode() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginToken() == "" {
		return "", errors.New("web: login token is not initialized")
	}
	now := s.launchTime()
	for code, expiry := range s.launchCodes {
		if !now.Before(expiry) {
			delete(s.launchCodes, code)
		}
	}
	if s.launchCodes == nil {
		s.launchCodes = make(map[string]time.Time)
	}
	if len(s.launchCodes) >= 64 {
		return "", errors.New("web: too many pending launch codes")
	}
	code := hex.EncodeToString(buf[:])
	s.launchCodes[code] = now.Add(time.Minute)
	return code, nil
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	code := r.URL.Query().Get("code")
	s.mu.Lock()
	expiry, ok := s.launchCodes[code]
	delete(s.launchCodes, code)
	valid := ok && s.launchTime().Before(expiry)
	s.mu.Unlock()
	if !valid {
		http.Error(w, "invalid launch code", http.StatusForbidden)
		return
	}
	s.setCookie(w, s.loginToken())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
