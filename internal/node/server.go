package node

import (
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

	"xbot/internal/fsutil"
)

// Info is what GET /v1/node returns.
type Info struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Caps []string `json:"caps"`
}

// Caps are the capabilities this process can run. Shell waits for the machine switch.
func Caps() []string { return []string{"info"} }

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

// Handler serves /v1/node and /v1/call. call runs a capability. Nil uses info only.
func Handler(token string, info Info, call func(cap string) (string, error)) http.Handler {
	token = strings.TrimSpace(token)
	if call == nil {
		call = func(cap string) (string, error) {
			if cap == "info" {
				return InfoText(info), nil
			}
			return "", fmt.Errorf("node: unknown cap %q", cap)
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
		var in struct {
			Cap string `json:"cap"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "bad request")
			return
		}
		text, err := call(strings.TrimSpace(in.Cap))
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
