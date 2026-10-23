package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

// OAuthCredential is one atomic vault value. Neither List nor the daemon's
// control responses expose its tokens. Normal Inject substitutes AccessToken.
type OAuthCredential struct {
	OAuthVersion int       `json:"xbot_oauth"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	Resource     string    `json:"resource,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	Scope        string    `json:"scope,omitempty"`
}

// OAuthInfo contains only metadata safe for configuration and UI.
type OAuthInfo struct {
	Configured  bool      `json:"configured"`
	Refreshable bool      `json:"refreshable"`
	ClientID    string    `json:"client_id,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

type oauthBackend interface {
	ensureOAuth(context.Context, string) (OAuthInfo, error)
	oauthInfo(context.Context, string) (OAuthInfo, error)
}

func OAuthMetadata(ctx context.Context, b Backend, name string) (OAuthInfo, error) {
	if b, ok := b.(oauthBackend); ok {
		return b.oauthInfo(ctx, name)
	}
	return OAuthInfo{}, errors.New("vault: OAuth backend unsupported")
}
func (s *Store) oauthInfo(ctx context.Context, name string) (OAuthInfo, error) {
	err := s.withFileLock(func() error { s.mu.Lock(); defer s.mu.Unlock(); return s.reloadLocked() })
	if err != nil {
		return OAuthInfo{}, err
	}
	sec, ok := s.Lookup(name)
	if !ok {
		return OAuthInfo{}, nil
	}
	c, ok := oauthValue(sec.Value)
	if !ok {
		return OAuthInfo{}, errors.New("vault: invalid OAuth credential")
	}
	return OAuthInfo{Configured: c.AccessToken != "", Refreshable: c.RefreshToken != "", ClientID: c.ClientID, Subject: c.Subject, ExpiresAt: c.ExpiresAt}, nil
}
func (c *Client) oauthInfo(ctx context.Context, name string) (OAuthInfo, error) {
	raw, err := c.control(ctx, "POST", "/v1/oauth/info", mustOAuthJSON(map[string]string{"name": name}))
	var out OAuthInfo
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

func oauthValue(value string) (OAuthCredential, bool) {
	var c OAuthCredential
	if !strings.HasPrefix(value, "{") || json.Unmarshal([]byte(value), &c) != nil || c.OAuthVersion != 1 {
		return c, false
	}
	return c, true
}

// SetOAuth uses the existing Set/encryption/domain machinery. The same lock as
// refresh prevents a late refresh from overwriting a completed new sign-in.
func SetOAuth(b Backend, name string, c OAuthCredential, domains []string) error {
	c.OAuthVersion = 1
	if strings.TrimSpace(c.AccessToken) == "" {
		return errors.New("vault: OAuth access token missing")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if s, ok := b.(*Store); ok {
		return fsutil.WithLock(filepath.Join(s.dir, "oauth.lock"), func() error { return s.Set(name, string(raw), domains) })
	}
	if c, ok := b.(*Client); ok {
		_, err = c.control(context.Background(), http.MethodPost, "/v1/oauth/set", mustOAuthJSON(setRequest{Name: name, Value: string(raw), Domains: domains}))
		return err
	}
	return errors.New("vault: OAuth backend unsupported")
}

func mustOAuthJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

// EnsureOAuth refreshes near expiry and returns metadata only. Refresh is
// serialized across processes, rereads disk, and atomically replaces all tokens.
func EnsureOAuth(ctx context.Context, b Backend, name string) (OAuthInfo, error) {
	if b, ok := b.(oauthBackend); ok {
		return b.ensureOAuth(ctx, name)
	}
	return OAuthInfo{}, errors.New("vault: OAuth backend unsupported")
}

func (s *Store) ensureOAuth(ctx context.Context, name string) (info OAuthInfo, err error) {
	err = fsutil.WithLock(filepath.Join(s.dir, "oauth.lock"), func() error {
		if err := s.withFileLock(func() error { s.mu.Lock(); defer s.mu.Unlock(); return s.reloadLocked() }); err != nil {
			return err
		}
		sec, ok := s.Lookup(name)
		if !ok {
			return errors.New("vault: OAuth login required")
		}
		c, ok := oauthValue(sec.Value)
		if !ok || c.AccessToken == "" {
			return errors.New("vault: invalid OAuth credential")
		}
		if !c.ExpiresAt.IsZero() && time.Until(c.ExpiresAt) < 30*time.Second {
			if c.RefreshToken == "" {
				return errors.New("vault: OAuth credential expired; sign in again")
			}
			if err := s.refreshOAuth(ctx, &c, sec.Domains); err != nil {
				return err
			}
			raw, _ := json.Marshal(c)
			if err := s.Set(name, string(raw), sec.Domains); err != nil {
				return err
			}
		}
		info = OAuthInfo{Configured: true, Refreshable: c.RefreshToken != "", ClientID: c.ClientID, Subject: c.Subject, ExpiresAt: c.ExpiresAt}
		return nil
	})
	return
}

func (s *Store) refreshOAuth(ctx context.Context, c *OAuthCredential, domains []string) error {
	u, err := url.Parse(c.TokenURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !HostAllowed(u.Hostname(), domains) || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return errors.New("vault: invalid OAuth token endpoint")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {c.ClientID}, "refresh_token": {c.RefreshToken}, "resource": {c.Resource}}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("vault: cannot build OAuth refresh")
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h := &http.Client{Transport: s.Transport(nil), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := h.Do(r)
	if err != nil {
		return errors.New("vault: OAuth refresh transport failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("vault: OAuth refresh rejected (%d); sign in again", res.StatusCode)
	}
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		ID      string `json:"id_token"`
		Type    string `json:"token_type"`
		Expires int    `json:"expires_in"`
		Scope   string `json:"scope"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&token) != nil || token.Access == "" || token.Expires <= 30 || !strings.EqualFold(token.Type, "Bearer") {
		return errors.New("vault: invalid OAuth refresh response")
	}
	if token.Scope != "" && !strings.Contains(" "+token.Scope+" ", " chatgpt.tokens.use.direct ") {
		return errors.New("vault: ChatGPT plan scope missing")
	}
	c.AccessToken = token.Access
	if token.Refresh != "" {
		c.RefreshToken = token.Refresh
	}
	if token.ID != "" {
		c.IDToken = token.ID
	}
	if token.Scope != "" {
		c.Scope = token.Scope
	}
	c.ExpiresAt = time.Now().Add(time.Duration(token.Expires) * time.Second)
	return nil
}

func (c *Client) ensureOAuth(ctx context.Context, name string) (OAuthInfo, error) {
	raw, err := c.control(ctx, "POST", "/v1/oauth/ensure", mustOAuthJSON(map[string]string{"name": name}))
	var out OAuthInfo
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}
