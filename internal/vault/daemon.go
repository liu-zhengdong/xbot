package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Wire protocol constants. The daemon speaks HTTP/1.1 over a unix socket.
// Control data travels in small headers; bodies are streamed.
const (
	headerBot     = "X-Vault-Bot"
	headerMethod  = "X-Vault-Method"
	headerURL     = "X-Vault-URL"
	headerHost    = "X-Vault-Host"
	headerStatus  = "X-Vault-Status"
	headerHPrefix = "X-Vault-H-"

	// maxSetBody caps a /v1/set body.
	maxSetBody = 64 << 10
	// maxForwardBody caps the request body that may be buffered for
	// substitution because it carries a placeholder.
	maxForwardBody = 1 << 20
	// maxForwardResp caps a forwarded response body; past it the stream is cut.
	maxForwardResp = 8 << 20
)

// botNameRE validates the X-Vault-Bot value before it is used as a path element.
var botNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// placeholderLead is the literal opening of a secret placeholder.
const placeholderLead = "{{secret:"

// Server is the vault daemon. It holds every bot's vault under DataDir and only
// serves peers whose uid is in AllowUIDs.
type Server struct {
	// DataDir is the daemon's data root: per-bot vaults at DataDir/<bot>/ and
	// keys at DataDir/keys/<bot>.key.
	DataDir string
	// AllowUIDs is the set of peer uids allowed to connect.
	AllowUIDs []int
	// Base is the transport used for forwarded requests. Nil uses
	// http.DefaultTransport.
	Base http.RoundTripper

	mu     sync.Mutex
	stores map[string]*Store
}

// Serve runs a daemon on ln with dataDir as its data root. It blocks until the
// listener is closed. Every accepted connection has its peer uid checked once.
func Serve(ln net.Listener, dataDir string, allowUIDs []int) error {
	s := &Server{DataDir: dataDir, AllowUIDs: allowUIDs}
	return s.Serve(ln)
}

// Serve runs the daemon on ln. It blocks until the listener is closed.
func (s *Server) Serve(ln net.Listener) error {
	if s.DataDir == "" {
		return errors.New("vault: empty data dir")
	}
	if err := os.MkdirAll(s.DataDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(s.DataDir, 0700); err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.Serve(&trustedListener{Listener: ln, allowUIDs: s.AllowUIDs})
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/list", s.handleList)
	mux.HandleFunc("/v1/set", s.handleSet)
	mux.HandleFunc("/v1/rm", s.handleRm)
	mux.HandleFunc("/v1/forward", s.handleForward)
	return mux
}

// trustedListener performs the one-time peer check: a connection whose peer uid
// is not allowed is closed and never read from.
type trustedListener struct {
	net.Listener
	allowUIDs []int
}

func (l *trustedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(c)
		if err != nil || !uidAllowed(l.allowUIDs, uid) {
			_ = c.Close()
			if err != nil {
				// Off-Linux every peer fails; back off so Accept cannot spin.
				time.Sleep(10 * time.Millisecond)
			}
			continue
		}
		return c, nil
	}
}

func uidAllowed(allow []int, uid int) bool {
	for _, a := range allow {
		if a == uid {
			return true
		}
	}
	return false
}

// storeFor returns the per-bot store, opening and caching it on first use.
func (s *Server) storeFor(bot string) (*Store, error) {
	if !botNameRE.MatchString(bot) {
		return nil, errors.New("vault: bad bot name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores == nil {
		s.stores = map[string]*Store{}
	}
	if st, ok := s.stores[bot]; ok {
		return st, nil
	}
	dir := filepath.Join(s.DataDir, bot)
	keyPath := filepath.Join(s.DataDir, "keys", bot+".key")
	st, err := Open(dir, keyPath)
	if err != nil {
		return nil, err
	}
	s.stores[bot] = st
	return st, nil
}

func (s *Server) botOf(w http.ResponseWriter, r *http.Request) (*Store, bool) {
	bot := strings.TrimSpace(r.Header.Get(headerBot))
	st, err := s.storeFor(bot)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return nil, false
	}
	return st, true
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("vault: method not allowed"))
		return
	}
	st, ok := s.botOf(w, r)
	if !ok {
		return
	}
	type entry struct {
		Name    string   `json:"name"`
		Domains []string `json:"domains"`
	}
	list := st.List()
	out := make([]entry, 0, len(list))
	for _, sec := range list {
		out = append(out, entry{Name: sec.Name, Domains: append([]string{}, sec.Domains...)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type setRequest struct {
	Name    string   `json:"name"`
	Value   string   `json:"value"`
	Domains []string `json:"domains"`
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("vault: method not allowed"))
		return
	}
	st, ok := s.botOf(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSetBody)
	var in setRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("vault: bad set request"))
		return
	}
	if err := st.Set(in.Name, in.Value, in.Domains); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("vault: method not allowed"))
		return
	}
	st, ok := s.botOf(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSetBody)
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("vault: bad rm request"))
		return
	}
	if err := st.Remove(in.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("vault: method not allowed"))
		return
	}
	st, ok := s.botOf(w, r)
	if !ok {
		return
	}
	method := strings.TrimSpace(r.Header.Get(headerMethod))
	if method == "" {
		method = http.MethodGet
	}
	rawURL := strings.TrimSpace(r.Header.Get(headerURL))
	if rawURL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("vault: missing target"))
		return
	}
	header := http.Header{}
	for name, values := range r.Header {
		if !strings.HasPrefix(name, headerHPrefix) {
			continue
		}
		real := strings.TrimPrefix(name, headerHPrefix)
		if real == "" {
			continue
		}
		for _, v := range values {
			header.Add(real, v)
		}
	}
	// Decide the body mode. Only a placeholder-bearing body is buffered.
	buf, rest, hasPH, tooBig, err := readForwardBody(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if tooBig {
		writeErr(w, http.StatusRequestEntityTooLarge, errors.New("vault: request body too large"))
		return
	}
	var reader io.Reader
	if rest != nil {
		reader = io.MultiReader(bytes.NewReader(buf), rest)
	} else {
		reader = bytes.NewReader(buf)
	}
	target, err := http.NewRequestWithContext(r.Context(), method, rawURL, reader)
	if err != nil {
		writeErr(w, http.StatusBadRequest, st.sanitizeError(err))
		return
	}
	target.Header = header
	target.Host = r.Header.Get(headerHost)
	if rest != nil {
		target.ContentLength = r.ContentLength
	} else {
		target.ContentLength = int64(len(buf))
	}
	var out *http.Request
	if hasPH || rest == nil {
		out, err = st.Inject(target)
	} else {
		out, err = st.InjectStream(target, buf)
	}
	if err != nil {
		writeErr(w, http.StatusForbidden, st.sanitizeError(err))
		return
	}
	base := s.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(out)
	if err != nil {
		writeErr(w, http.StatusBadGateway, st.sanitizeError(err))
		return
	}
	defer resp.Body.Close()
	w.Header().Set(headerStatus, strconv.Itoa(resp.StatusCode))
	for name, values := range resp.Header {
		for _, v := range values {
			// A server can echo the value into a header (Location, ETag, ...);
			// redact every forwarded header value as well as the body.
			w.Header().Add(headerHPrefix+name, st.Redact(v))
		}
	}
	w.WriteHeader(http.StatusOK)
	stream := newRedactStream(w, st, maxForwardResp)
	if flusher, ok := w.(http.Flusher); ok {
		stream.flushNow = flusher.Flush
	}
	_, _ = io.Copy(stream, resp.Body)
	stream.flush()
}

// readForwardBody buffers a request body only far enough to see whether it
// carries a placeholder. If it does and is larger than the cap, tooBig is set.
// Otherwise a body larger than the cap is returned as a prefix plus a reader
// for the remainder, to be streamed untouched.
//
// A placeholder that begins past maxForwardBody is not detected and travels
// literally; callers keep placeholders well inside the first 1 MiB.
func readForwardBody(body io.Reader) (buf []byte, rest io.Reader, hasPH, tooBig bool, err error) {
	if body == nil {
		return nil, nil, false, false, nil
	}
	// Read past the cap by one placeholder lead so a placeholder that starts
	// exactly at the cap edge is still detected.
	head, err := io.ReadAll(io.LimitReader(body, maxForwardBody+int64(len(placeholderLead))))
	if err != nil {
		return nil, nil, false, false, err
	}
	hasPH = bytes.Contains(head, []byte(placeholderLead))
	if len(head) > maxForwardBody {
		if hasPH {
			return nil, nil, true, true, nil
		}
		return head, body, false, false, nil
	}
	return head, nil, hasPH, false, nil
}

// redactStream redacts a forwarded response as it streams. It keeps an overlap
// window of max-value-form-length-1 bytes so a value split across chunk
// boundaries is still hidden, and flushes the tail at EOF. It cuts the stream
// at a hard cap; the tail is dropped, never emitted raw.
type redactStream struct {
	w       io.Writer
	store   *Store
	overlap int

	// flushNow pushes a redacted piece to the peer immediately; nil is a no-op.
	flushNow func()

	pending   string
	written   int64
	limit     int64
	truncated bool
}

func newRedactStream(w io.Writer, store *Store, limit int64) *redactStream {
	overlap := 0
	if store != nil {
		if n := store.MaxFormLen(); n > 0 {
			overlap = n - 1
		}
	}
	return &redactStream{w: w, store: store, overlap: overlap, limit: limit}
}

func (rs *redactStream) Write(p []byte) (int, error) {
	if rs.truncated {
		return len(p), nil
	}
	rs.pending += string(p)
	red := rs.pending
	if rs.store != nil {
		red = rs.store.Redact(rs.pending)
	}
	if len(red) <= rs.overlap {
		rs.pending = red
		return len(p), nil
	}
	emit := red[:len(red)-rs.overlap]
	rs.pending = red[len(red)-rs.overlap:]
	return len(p), rs.emit(emit)
}

func (rs *redactStream) flush() {
	if rs.truncated {
		rs.pending = ""
		return
	}
	if rs.pending == "" {
		return
	}
	tail := rs.pending
	rs.pending = ""
	_ = rs.emit(tail)
}

func (rs *redactStream) emit(s string) error {
	if rs.limit > 0 && rs.written+int64(len(s)) > rs.limit {
		room := rs.limit - rs.written
		if room < 0 {
			room = 0
		}
		s = s[:room]
		rs.truncated = true
	}
	if s == "" {
		return nil
	}
	n, err := io.WriteString(rs.w, s)
	rs.written += int64(n)
	if rs.flushNow != nil {
		rs.flushNow()
	}
	return err
}

// MaxFormLen is the longest redactable value form, used as a streaming
// overlap. Zero when no value is stored. Encoded forms (base64, URL, JSON)
// count; they are bounded by the stored value.
func (s *Store) MaxFormLen() int {
	best := 0
	for _, form := range s.valueForms() {
		if len(form) > best {
			best = len(form)
		}
	}
	return best
}

func writeErr(w http.ResponseWriter, status int, err error) {
	msg := "vault: request failed"
	if err != nil {
		msg = err.Error()
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, strings.TrimSpace(msg))
}
