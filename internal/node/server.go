package node

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

// Info is what GET /v1/node returns.
type Info struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Caps []string `json:"caps"`
}

// Caps are the capabilities this process can run.
// shell still consults the machine switch on each call.
func Caps() []string { return []string{"info", "shell"} }

// Request is one POST /v1/call.
type Request struct {
	Cap     string `json:"cap"`
	Command string `json:"command,omitempty"`
	// Approved is accepted so older clients are not rejected. This node ignores it.
	Approved bool `json:"approved,omitempty"`
	// Caller is the remote address. The handler overwrites any client-supplied value.
	Caller string `json:"caller,omitempty"`
}

func idPath(root string) string    { return filepath.Join(root, "node-id") }
func tokenPath(root string) string { return filepath.Join(root, "node-token") }

// LoadID returns the stable id for this data root, creating it on first use.
func LoadID(root string) (string, error) {
	raw, err := os.ReadFile(idPath(root))
	if err == nil {
		id := strings.TrimSpace(string(raw))
		if id != "" {
			return id, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := fsutil.WriteAtomic(idPath(root), []byte(id+"\n"), 0600); err != nil {
		return "", err
	}
	return id, nil
}

// ResolveToken picks a bearer token. "-" reads one line from stdin.
// An empty flag uses env. An empty result means the caller should generate one.
func ResolveToken(flag, env string, stdin io.Reader) (string, error) {
	flag = strings.TrimSpace(flag)
	switch {
	case flag == "-":
		if stdin == nil {
			return "", errors.New("node: empty token")
		}
		sc := bufio.NewScanner(stdin)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", err
			}
			return "", errors.New("node: empty token")
		}
		tok := strings.TrimSpace(sc.Text())
		if tok == "" {
			return "", errors.New("node: empty token")
		}
		return tok, nil
	case flag != "":
		return flag, nil
	default:
		return strings.TrimSpace(env), nil
	}
}

// Serve listens on addr until ctx is cancelled, then shuts down.
func Serve(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// EnsureToken returns the flag value, or the stored node token, or a new one.
// created is true when a new token was written.
func EnsureToken(root, flag string) (token string, created bool, err error) {
	flag = strings.TrimSpace(flag)
	if flag != "" {
		return flag, false, nil
	}
	raw, err := os.ReadFile(tokenPath(root))
	if err == nil {
		token = strings.TrimSpace(string(raw))
		if token != "" {
			return token, false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(b[:])
	if err := fsutil.WriteAtomic(tokenPath(root), []byte(token+"\n"), 0600); err != nil {
		return "", false, err
	}
	return token, true, nil
}

// Identity is this machine's node description.
func Identity(root, name string) (Info, error) {
	id, err := LoadID(root)
	if err != nil {
		return Info{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		host, err := os.Hostname()
		if err != nil || strings.TrimSpace(host) == "" {
			name = "node"
		} else {
			name = host
		}
	}
	return Info{ID: id, Name: name, Caps: Caps()}, nil
}

// InfoText is the body of a successful info call.
func InfoText(info Info) string {
	return fmt.Sprintf("id\t%s\nname\t%s\nos\t%s\narch\t%s", info.ID, info.Name, runtime.GOOS, runtime.GOARCH)
}

// Handler serves /v1/node and /v1/call. call runs a capability. Nil handles info only.
func Handler(token string, info Info, call func(Request) (string, error)) http.Handler {
	token = strings.TrimSpace(token)
	if call == nil {
		call = func(req Request) (string, error) {
			if req.Cap == "info" {
				return InfoText(info), nil
			}
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method")
			return
		}
		if !authorized(r, token) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("/v1/call", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method")
			return
		}
		if !authorized(r, token) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in Request
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "bad request")
			return
		}
		in.Cap = strings.TrimSpace(in.Cap)
		in.Caller = r.RemoteAddr
		text, err := call(in)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "unknown cap") {
				status = http.StatusNotFound
			}
			writeErr(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"text": text})
	})
	return mux
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == r.Header.Get("Authorization") {
		return false
	}
	return subtleEqual(got, token)
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}
