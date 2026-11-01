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

// MaxAvatarBody is the raw upload limit, separate from JSON profile metadata.
const MaxAvatarBody = 1 << 20

// Caller identifies one existing runtime. Bot is diagnostic; Endpoint owns self.
type Caller struct{ Binary, Root, Endpoint, Bot string }

// Env replaces inherited caller information rather than leaking a parent Bot.
func (c Caller) Env(base []string) []string {
	out := make([]string, 0, len(base)+4)
	for _, item := range base {
		name, _, _ := strings.Cut(item, "=")
		switch name {
		case "XBOT_BIN", "XBOT_ROOT", "XBOT_ENDPOINT", "XBOT_BOT", "XBOT_CALL_ID":
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
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	AvatarSeed  *string `json:"avatar_seed,omitempty"`
}

type Profile struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Timezone      string   `json:"timezone"`
	Avatar        string   `json:"avatar"`
	AvatarSeed    string   `json:"avatar_seed"`
	Scope         string   `json:"scope"`
	Applied       bool     `json:"applied"`
	PreviousName  string   `json:"previous_name,omitempty"`
	AppliedFields []string `json:"applied_fields,omitempty"`
	Moved         bool     `json:"moved"`
	Error         string   `json:"error,omitempty"`
}

// Server is owned by the Kernel whose profile callback it serves.
type Server struct {
	Endpoint string
	http     *http.Server
	done     chan struct{}
}

func Start(root string, handlers Handlers) (*Server, error) {
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
		if handlers.Profile == nil {
			fail(404, "profile unavailable")
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
			if update.Name == nil && update.Description == nil && update.Timezone == nil && update.AvatarSeed == nil {
				fail(400, "profile: nothing to change")
				return
			}
		default:
			fail(405, "method not allowed")
			return
		}
		value, err := handlers.Profile(update)
		writeProfile(w, value, err)
	})
	mux.HandleFunc("/v1/profile/avatar", func(w http.ResponseWriter, r *http.Request) {
		fail := func(code int, message string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		if handlers.ProfileAvatar == nil {
			fail(http.StatusNotFound, "profile avatar unavailable")
			return
		}
		clear := r.Method == http.MethodDelete
		if r.Method != http.MethodPost && !clear {
			fail(http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxAvatarBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				fail(http.StatusRequestEntityTooLarge, "profile avatar: upload exceeds 1 MiB")
			} else {
				fail(http.StatusBadRequest, "invalid avatar request")
			}
			return
		}
		if clear && len(data) != 0 {
			fail(http.StatusBadRequest, "profile avatar: clear does not accept an upload")
			return
		}
		// MIME comes from the domain's sniff of these bytes, never Content-Type.
		value, err := handlers.ProfileAvatar(data, clear)
		writeProfile(w, value, err)
	})
	mux.HandleFunc("/v1/mcp/", func(w http.ResponseWriter, r *http.Request) {
		if handlers.MCP == nil {
			http.NotFound(w, r)
			return
		}
		ServeMCP(w, r, *handlers.MCP, strings.TrimPrefix(r.URL.Path, "/v1/mcp/"))
	})
	mux.HandleFunc("/v1/skill/", func(w http.ResponseWriter, r *http.Request) {
		serveSkill(w, r, handlers.Skills)
	})
	mux.HandleFunc("/v1/routine/", func(w http.ResponseWriter, r *http.Request) {
		serveRoutine(w, r, handlers.Routines)
	})
	mux.HandleFunc("/v1/bot/", func(w http.ResponseWriter, r *http.Request) {
		serveBot(w, r, handlers.Bots)
	})
	mux.HandleFunc("/v1/chat-group/", func(w http.ResponseWriter, r *http.Request) {
		serveChatGroup(w, r, handlers.ChatGroups)
	})
	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(code int, message string) {
			w.Header().Set("Content-Type", "application/json")
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
		mux.ServeHTTP(w, r)
	})
	s.http = &http.Server{Handler: guarded, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second}
	go func() { defer close(s.done); _ = s.http.Serve(ln) }()
	return s, nil
}

// writeProfile preserves the domain's actual result even when a later step
// fails. No domain result is fabricated for errors rejected before a change.
func writeProfile(w http.ResponseWriter, value Profile, problem error) {
	status := http.StatusOK
	if problem != nil {
		status = http.StatusBadRequest
		value.Error = problem.Error()
		if len(value.Error) > 4096 {
			value.Error = value.Error[:4096] + "…"
		}
	}
	var response any = value
	if problem != nil && value.Name == "" {
		response = map[string]string{"error": value.Error}
	}
	raw, err := json.Marshal(response)
	if err != nil || len(raw)+1 > maxBody {
		status = http.StatusInternalServerError
		raw = []byte(`{"error":"profile: runtime response too large"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
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
	root, err := sourceRoot(endpoint, root, "profile")
	if err != nil {
		return result, err
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
	return callProfile(req)
}

// CallAvatar streams one bounded raw upload, or sends an empty DELETE for clear.
// It uses the same source identity, response budget and partial DTO as Call.
func CallAvatar(ctx context.Context, endpoint, root string, data io.Reader, clear bool) (Profile, error) {
	root, err := sourceRoot(endpoint, root, "profile")
	if err != nil {
		return Profile{}, err
	}
	method := http.MethodPost
	var body io.Reader
	if clear {
		method = http.MethodDelete
	} else {
		if data == nil {
			return Profile{}, errors.New("profile avatar: upload requires data")
		}
		body = io.LimitReader(data, MaxAvatarBody+1)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+"/v1/profile/avatar", body)
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("X-XBot-Root", root)
	if !clear {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return callProfile(req)
}

func sourceRoot(endpoint, root, operation string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("%s: no runtime endpoint; --self requires an active XBot shell", operation)
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return "", fmt.Errorf("%s: invalid local runtime endpoint", operation)
	}
	root, err = CanonicalRoot(root)
	if err != nil {
		return "", fmt.Errorf("%s: data root: %w", operation, err)
	}
	return root, nil
}

func callProfile(req *http.Request) (Profile, error) {
	var result Profile
	client := LocalHTTPClient(10 * time.Second)
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
	if err := json.Unmarshal(raw, &result); err != nil {
		return Profile{}, errors.New("profile: invalid runtime response")
	}
	if result.Error != "" {
		return result, fmt.Errorf("profile: %s", result.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("profile: runtime returned HTTP %d", resp.StatusCode)
	}
	return result, nil
}
