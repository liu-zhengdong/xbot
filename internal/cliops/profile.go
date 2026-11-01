// Package cliops provides the narrow local profile protocol shared by serve,
// desktop and standalone chat. It neither starts kernels nor dispatches tools.
package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const maxBody = 64 << 10

// Caller identifies one existing runtime. Bot is diagnostic; Endpoint owns self.
type Caller struct{ Binary, Root, Endpoint, Bot string }

// Env replaces inherited caller information rather than leaking a parent Bot.
func (c Caller) Env(base []string) []string {
	out := make([]string, 0, len(base)+4)
	for _, item := range base {
		name, _, _ := strings.Cut(item, "=")
		switch name {
		case "XBOT_BIN", "XBOT_ROOT", "XBOT_ENDPOINT", "XBOT_BOT":
			continue
		}
		out = append(out, item)
	}
	return append(out, "XBOT_BIN="+c.Binary, "XBOT_ROOT="+c.Root, "XBOT_ENDPOINT="+c.Endpoint, "XBOT_BOT="+c.Bot)
}

func CanonicalRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

type Update struct {
	Description *string `json:"description,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
}

type Profile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Timezone    string `json:"timezone"`
	Scope       string `json:"scope"`
	Applied     bool   `json:"applied"`
}

// Server is owned by the Kernel whose profile callback it serves.
type Server struct {
	Endpoint string
	http     *http.Server
	done     chan struct{}
}

func Start(root string, profile func(*Update) (Profile, error)) (*Server, error) {
	root, err := CanonicalRoot(root)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{Endpoint: "http://" + ln.Addr().String(), done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/profile", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fail := func(code int, message string) {
			// Validation errors can quote a large invalid timezone. Keep even
			// their escaped JSON comfortably inside the response budget.
			if len(message) > 4096 {
				message = message[:4096] + "…"
			}
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			fail(403, "browser requests are not supported")
			return
		}
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			fail(403, "local endpoint required")
			return
		}
		if r.Header.Get("X-XBot-Root") != root {
			fail(409, "runtime data root mismatch")
			return
		}
		var update *Update
		switch r.Method {
		case http.MethodGet:
		case http.MethodPatch:
			update = &Update{}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(update); err != nil {
				fail(400, "invalid profile request")
				return
			}
			if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
				fail(400, "invalid profile request")
				return
			}
			if update.Description == nil && update.Timezone == nil {
				fail(400, "profile: nothing to change")
				return
			}
		default:
			fail(405, "method not allowed")
			return
		}
		value, err := profile(update)
		if err != nil {
			fail(400, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	})
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second}
	go func() { defer close(s.done); _ = s.http.Serve(ln) }()
	return s, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	err := s.http.Close()
	<-s.done
	return err
}

// Call only talks to an explicitly supplied loopback runtime; it never loads
// data from disk or falls back to another instance on failure.
func Call(ctx context.Context, endpoint, root string, update *Update) (Profile, error) {
	var result Profile
	if endpoint == "" {
		return result, errors.New("profile: no runtime endpoint; --self requires an active XBot shell")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return result, errors.New("profile: invalid local runtime endpoint")
	}
	root, err = CanonicalRoot(root)
	if err != nil {
		return result, fmt.Errorf("profile: data root: %w", err)
	}
	method := http.MethodGet
	var body io.Reader
	if update != nil {
		method = http.MethodPatch
		raw, err := json.Marshal(update)
		if err != nil {
			return result, err
		}
		if len(raw) > maxBody {
			return result, errors.New("profile: request too large")
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/profile", body)
	if err != nil {
		return result, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return result, errors.New("profile: runtime endpoint unavailable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return result, errors.New("profile: invalid runtime response")
	}
	if resp.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &failure) == nil && failure.Error != "" {
			return result, fmt.Errorf("profile: %s", failure.Error)
		}
		return result, fmt.Errorf("profile: runtime returned HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("profile: invalid runtime response")
	}
	return result, nil
}
