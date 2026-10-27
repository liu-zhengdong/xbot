package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/person"
)

const cookieName = "xbot_session"

type secretFile struct {
	Token   string    `json:"token"`
	Owner   string    `json:"owner"`
	Created time.Time `json:"created"`
}

func (s *Server) tokenPath() string {
	return filepath.Join(s.Root, "web-token.json")
}

// EnsureToken loads or creates the owner login token. The file is mode 0600.
// Owner, when set before the first call, is stored instead of the terminal user.
func (s *Server) EnsureToken() (string, bool, error) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	raw, err := os.ReadFile(s.tokenPath())
	if err == nil {
		var f secretFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return "", false, err
		}
		if f.Token == "" || f.Owner == "" {
			return "", false, errors.New("web: broken token file")
		}
		s.owner = f.Owner
		s.token = f.Token
		return f.Token, false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	owner := s.Owner
	if owner == "" {
		ext, display, err := link.Account()
		if err != nil {
			return "", false, err
		}
		who, err := person.Identify(s.Root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
		if err != nil {
			return "", false, err
		}
		owner = who.ID
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", false, err
	}
	f := secretFile{Token: hex.EncodeToString(buf[:]), Owner: owner, Created: time.Now().UTC()}
	raw, err = fsutil.Marshal(f)
	if err != nil {
		return "", false, err
	}
	if err := fsutil.WriteAtomic(s.tokenPath(), raw, 0600); err != nil {
		return "", false, err
	}
	s.owner = f.Owner
	s.token = f.Token
	return f.Token, true, nil
}

func (s *Server) checkToken(got string) bool {
	token := s.loginToken()
	if token == "" || len(got) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// loginToken is the token EnsureToken loaded, or "" before it ran.
func (s *Server) loginToken() string {
	s.authMu.RLock()
	defer s.authMu.RUnlock()
	return s.token
}

// AuthorizeOwner adds the owner's session cookie to an in-process request,
// for callers that already speak for the owner (the remote-console pipe).
// It only reads the token EnsureToken loaded at startup.
func (s *Server) AuthorizeOwner(r *http.Request) error {
	token := s.loginToken()
	if token == "" {
		return errors.New("web: login token is not initialized")
	}
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	return nil
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 30,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return s.checkToken(c.Value)
}

// allowOrigin rejects cross-site browser mutations. A missing Origin is allowed
// for non-browser clients; a present Origin must match the request host.
func allowOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	origin = strings.TrimPrefix(origin, "http://")
	origin = strings.TrimPrefix(origin, "https://")
	if i := strings.IndexByte(origin, '/'); i >= 0 {
		origin = origin[:i]
	}
	return strings.EqualFold(origin, r.Host)
}

// localBrowser identifies direct requests to the loopback address, excluding
// proxies and arbitrary Host names (including DNS rebinding).
func localBrowser(r *http.Request) bool {
	if forwarded(r) || !allowOrigin(r) {
		return false
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || net.ParseIP(peer) == nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}
