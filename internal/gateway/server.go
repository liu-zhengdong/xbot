package gateway

import (
	"cmp"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/fsutil"
)

const installIdleTTL = 30 * 24 * time.Hour

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)
var idRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

type limits struct {
	pairCount, recipientCount                          int
	pairBytes, recipientBytes, senderBytes, totalBytes int64
	pollBytes                                          int
}

type pollWaiter struct {
	ch       chan struct{}
	replaced bool
}

// Server stores signed mail for registered installations. Now defaults to time.Now.
type Server struct {
	Dir, Token string
	Open       bool
	// TrustForwarded trusts the last X-Forwarded-For address (set behind a proxy).
	TrustForwarded bool
	Now            func() time.Time
	mu             sync.Mutex
	waiters        map[string]*pollWaiter
	limits         *limits // Optional test bounds.
	totalBytes     int64
	senderBytes    map[string]int64
	counted        bool

	pendingSweep time.Time
	codeFailures map[string][]time.Time
	codeTries    []time.Time
}

type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

type route func(*http.Request, []byte, string, ed25519.PublicKey) (int, any, error)

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	// Browsers opening the gateway get the XBot mark (the Worker serves the same).
	for _, path := range []string{"GET /favicon.svg", "GET /favicon.ico"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			_, _ = w.Write(faviconSVG)
		})
	}
	for path, fn := range map[string]route{"POST /v1/register": s.register, "POST /v1/send": s.send, "GET /v1/poll": s.servePoll, "POST /v1/ack": s.ack, "POST /v1/code": s.registerCode, "POST /v1/code/cancel": s.cancelCode, "POST /v1/code/claim": s.claimCode, "POST /v1/code/admit": s.admitCode} {
		mux.Handle(path, s.verified(path == "POST /v1/register" || path == "POST /v1/code/claim", fn))
	}
	return mux
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) verified(allowUnregistered bool, fn route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, out, err := func() (int, any, error) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
			if err != nil {
				return 0, nil, httpError{413, "body too large"}
			}
			pub, err := VerifyRequest(r, body, s.now(), Skew)
			if err != nil {
				return 0, nil, httpError{401, err.Error()}
			}
			caller := ID(pub)
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.sweepLocked(); err != nil {
				return 0, nil, err
			}
			path := filepath.Join(s.Dir, "installs", caller)
			registered, err := exists(path)
			if err != nil {
				return 0, nil, err
			}
			if registered {
				now := s.now()
				if err := os.Chtimes(path, now, now); err != nil {
					return 0, nil, err
				}
			} else {
				pending, err := s.pendingLocked(caller)
				if err != nil {
					return 0, nil, err
				}
				mail := r.URL.Path == "/v1/poll" || r.URL.Path == "/v1/ack" || r.URL.Path == "/v1/send"
				// A pending install (see pending.go) may only use its mailbox, or
				// register with the join token like anyone else.
				if (!allowUnregistered && (pending == nil || !mail)) ||
					(pending != nil && !mail && r.URL.Path != "/v1/register" && r.URL.Path != "/v1/code/claim") {
					return 0, nil, httpError{403, "not registered"}
				}
			}
			return fn(r, body, caller, pub)
		}()
		if err != nil {
			status = 500
			var he httpError
			if errors.As(err, &he) {
				status = he.status
			}
			out = map[string]string{"error": err.Error()}
		}
		writeJSON(w, status, out)
	})
}

func (s *Server) register(r *http.Request, _ []byte, _ string, pub ed25519.PublicKey) (int, any, error) {
	path := filepath.Join(s.Dir, "installs", ID(pub))
	ok, err := exists(path)
	if err != nil {
		return 0, nil, err
	}
	if !ok && !s.Open {
		got, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearer || s.Token == "" {
			return 0, nil, httpError{403, "registration closed"}
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
			return 0, nil, httpError{401, "bad token"}
		}
	}
	if err := s.admitLocked(pub); err != nil {
		return 0, nil, err
	}
	return 200, map[string]string{"status": "ok"}, nil
}

// admitLocked shares install capacity, eviction and last-seen updates across admission paths.
func (s *Server) admitLocked(pub ed25519.PublicKey) error {
	path := filepath.Join(s.Dir, "installs", ID(pub))
	ok, err := exists(path)
	if err != nil {
		return err
	}
	if !ok {
		files, err := s.filesLocked("installs", "", 0)
		if err != nil {
			return err
		}
		if len(files) >= MaxInstalls {
			slices.SortFunc(files, func(a, b queueFile) int {
				return a.info.ModTime().Compare(b.info.ModTime())
			})
			for _, f := range files {
				if s.now().Sub(f.info.ModTime()) <= installIdleTTL {
					break
				}
				mail, err := s.queueLocked(f.info.Name())
				if err != nil {
					return err
				}
				if len(mail) != 0 {
					continue
				}
				if err := s.deleteOwnerCodesLocked(f.info.Name()); err != nil {
					return err
				}
				if err := os.Remove(f.path); err != nil {
					return err
				}
				files = files[:len(files)-1]
				break
			}
			if len(files) >= MaxInstalls {
				return httpError{403, "too many installs"}
			}
		}
		if err := fsutil.WriteAtomic(path, pub, 0600); err != nil {
			return err
		}
	}
	if err := os.Chtimes(path, s.now(), s.now()); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.Dir, "pending", ID(pub))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) send(_ *http.Request, body []byte, caller string, pub ed25519.PublicKey) (int, any, error) {
	var env Envelope
	if json.Unmarshal(body, &env) != nil || env.V != 1 || !validID(env.ID) || !validID(env.To) || !kindRe.MatchString(env.Kind) || env.From != caller || env.Verify(pub) != nil {
		return 0, nil, httpError{400, "bad envelope"}
	}
	registered, err := exists(filepath.Join(s.Dir, "installs", caller))
	if err != nil {
		return 0, nil, err
	}
	if !registered {
		pending, err := s.pendingLocked(caller)
		if err != nil {
			return 0, nil, err
		}
		if pending == nil || env.To != pending.Owner {
			return 0, nil, httpError{403, "pending may only send to owner"}
		}
	}
	data, err := json.Marshal(env)
	if err != nil {
		return 0, nil, err
	}
	if len(data) > MaxBody {
		return 0, nil, httpError{413, "envelope too large"}
	}
	ok, err := exists(filepath.Join(s.Dir, "installs", env.To))
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		pending, err := s.pendingLocked(env.To)
		if err != nil {
			return 0, nil, err
		}
		if pending == nil {
			return 0, nil, httpError{404, "unknown recipient"}
		}
		if pending.Owner != caller {
			return 0, nil, httpError{403, "pending recipient belongs to another owner"}
		}
	}
	tombstones, err := s.filesLocked("acked", env.To, AckTTL)
	if err != nil {
		return 0, nil, err
	}
	files, err := s.queueLocked(env.To)
	if err != nil {
		return 0, nil, err
	}
	duplicate := false
	for _, f := range tombstones {
		duplicate = duplicate || f.info.Name() == env.ID
	}
	pairDir := filepath.Join(s.Dir, "queue", env.To, caller)
	pairCount, pairBytes, recipientBytes := 0, int64(0), int64(0)
	for _, f := range files {
		duplicate = duplicate || f.info.Name() == env.ID+".json"
		recipientBytes += f.info.Size()
		if filepath.Dir(f.path) == pairDir {
			pairCount++
			pairBytes += f.info.Size()
		}
	}
	if duplicate {
		return 200, map[string]string{"status": "duplicate"}, nil
	}
	l := s.bounds()
	size := int64(len(data))
	if s.totalBytes+size > l.totalBytes || s.senderBytes[caller]+size > l.senderBytes {
		if _, err := s.filesLocked("queue", "", TTL); err != nil {
			return 0, nil, err
		}
	}
	if pairCount+1 > l.pairCount || pairBytes+size > l.pairBytes || len(files)+1 > l.recipientCount || recipientBytes+size > l.recipientBytes || s.totalBytes+size > l.totalBytes || s.senderBytes[caller]+size > l.senderBytes {
		return 0, nil, httpError{507, "queue full"}
	}
	if err := fsutil.WriteAtomic(filepath.Join(pairDir, env.ID+".json"), data, 0600); err != nil {
		return 0, nil, err
	}
	s.totalBytes += size
	s.senderBytes[caller] += size
	if p := s.waiters[env.To]; p != nil {
		select {
		case p.ch <- struct{}{}:
		default:
		}
	}
	return 202, map[string]string{"status": "queued"}, nil
}

// servePoll runs with mu held, releasing it only while waiting.
func (s *Server) servePoll(r *http.Request, _ []byte, to string, _ ed25519.PublicKey) (int, any, error) {
	wait := 25
	if n, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil {
		wait = n
	}
	p := &pollWaiter{ch: make(chan struct{}, 1)}
	if old := s.waiters[to]; old != nil {
		old.replaced = true
		close(old.ch)
	}
	if s.waiters == nil {
		s.waiters = make(map[string]*pollWaiter)
	}
	s.waiters[to] = p
	defer func() {
		if s.waiters[to] == p {
			delete(s.waiters, to)
		}
	}()
	envs, err := s.listLocked(to)
	if err == nil && len(envs) == 0 && clampWait(wait) > 0 {
		s.mu.Unlock()
		timer := time.NewTimer(time.Duration(clampWait(wait)) * time.Second)
		select {
		case <-p.ch:
		case <-timer.C:
		case <-r.Context().Done():
		}
		timer.Stop()
		s.mu.Lock()
		if !p.replaced {
			registered, checkErr := exists(filepath.Join(s.Dir, "installs", to))
			if checkErr != nil {
				return 0, nil, checkErr
			}
			if !registered {
				pending, checkErr := s.pendingLocked(to)
				if checkErr != nil {
					return 0, nil, checkErr
				}
				if pending == nil {
					return 0, nil, httpError{403, "not registered"}
				}
			}
			envs, err = s.listLocked(to)
		}
	}
	if err != nil {
		return 0, nil, err
	}
	if envs == nil {
		envs = []json.RawMessage{}
	}
	return 200, map[string]any{"envelopes": envs}, nil
}

func (s *Server) ack(_ *http.Request, body []byte, caller string, _ ed25519.PublicKey) (int, any, error) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if json.Unmarshal(body, &in) != nil || len(in.IDs) > MaxAck {
		return 0, nil, httpError{400, "bad ids"}
	}
	for _, id := range in.IDs {
		if !validID(id) {
			return 0, nil, httpError{400, "bad ids"}
		}
	}
	if _, err := s.filesLocked("acked", caller, AckTTL); err != nil {
		return 0, nil, err
	}
	files, err := s.queueLocked(caller)
	if err != nil {
		return 0, nil, err
	}
	acked := 0
	for _, f := range files {
		if slices.Contains(in.IDs, strings.TrimSuffix(f.info.Name(), ".json")) {
			if err := s.removeLocked(f); err != nil {
				return 0, nil, err
			}
			path := filepath.Join(s.Dir, "acked", caller, strings.TrimSuffix(f.info.Name(), ".json"))
			if err := fsutil.WriteAtomic(path, nil, 0600); err != nil {
				return 0, nil, err
			}
			if err := os.Chtimes(path, s.now(), s.now()); err != nil {
				return 0, nil, err
			}
			acked++
		}
	}
	return 200, map[string]int{"acked": acked}, nil
}

func (s *Server) listLocked(to string) ([]json.RawMessage, error) {
	files, err := s.queueLocked(to)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b queueFile) int {
		return cmp.Or(a.info.ModTime().Compare(b.info.ModTime()), strings.Compare(a.path, b.path))
	})
	var out []json.RawMessage
	size := len("{\"envelopes\":[]}\n") - 1
	for _, f := range files {
		raw, err := os.ReadFile(f.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(json.RawMessage(raw))
		if err != nil {
			return nil, err
		}
		n := len(encoded) + 1
		if len(out) > 0 && size+n > s.bounds().pollBytes {
			break
		}
		out = append(out, encoded)
		size += n
		if len(out) == MaxPoll {
			break
		}
	}
	return out, nil
}

func (s *Server) queueLocked(to string) ([]queueFile, error) {
	if !s.counted {
		files, err := s.filesLocked("queue", "", 0)
		if err != nil {
			return nil, err
		}
		s.senderBytes = make(map[string]int64)
		for _, f := range files {
			s.senderBytes[filepath.Base(filepath.Dir(f.path))] += f.info.Size()
			s.totalBytes += f.info.Size()
		}
		s.counted = true
	}
	return s.filesLocked("queue", to, TTL)
}

func (s *Server) removeLocked(f queueFile) error {
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.totalBytes -= f.info.Size()
	sender := filepath.Base(filepath.Dir(f.path))
	s.senderBytes[sender] -= f.info.Size()
	if s.senderBytes[sender] == 0 {
		delete(s.senderBytes, sender)
	}
	return nil
}

func (s *Server) bounds() limits {
	if s.limits != nil {
		return *s.limits
	}
	return limits{64, MaxQueue, 1 << 20, MaxQueueBytes, 16 << 20, 256 << 20, MaxPollBytes}
}

type queueFile struct {
	path string
	info fs.FileInfo
}

// filesLocked scans one subtree, expiring files and updating queue accounting.
func (s *Server) filesLocked(root, sub string, ttl time.Duration) ([]queueFile, error) {
	var files []queueFile
	err := filepath.Walk(filepath.Join(s.Dir, root, sub), func(path string, info fs.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		f := queueFile{path, info}
		if ttl > 0 && s.now().Sub(info.ModTime()) >= ttl {
			if root == "queue" {
				return s.removeLocked(f)
			}
			if err := os.Remove(path); !os.IsNotExist(err) {
				return err
			}
		} else {
			files = append(files, f)
		}
		return nil
	})
	return files, err
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func validID(s string) bool { return idRe.MatchString(s) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
