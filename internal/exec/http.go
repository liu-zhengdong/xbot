package exec

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxBody = 8 << 20

// Server is the runner HTTP API. It accepts only loopback clients and a shared token.
type Server struct {
	Local *Local
	Token string
	// Code handles /v1/code/ routes after the loopback and bearer checks.
	Code http.Handler
}

// Handler is the runner's HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/start", s.start)
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		list, _ := s.Local.Tasks()
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("/v1/message", s.message)
	mux.HandleFunc("/v1/events", s.events)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackRemote(r.RemoteAddr) {
			http.Error(w, "runner: loopback only", http.StatusForbidden)
			return
		}
		if !s.authorized(r) {
			http.Error(w, "runner: unauthorized", http.StatusUnauthorized)
			return
		}
		if s.Code != nil && strings.HasPrefix(r.URL.Path, "/v1/code/") {
			s.Code.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	if s.Token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(got) != len(s.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var req StartRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	task, err := s.Local.Start(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var c Control
	if err := decode(w, r, &c); err != nil {
		return
	}
	if c.ID == "" {
		http.Error(w, "exec: id is required", http.StatusBadRequest)
		return
	}
	if err := s.Local.Control(r.Context(), c.ID, c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": c.ID, "status": "ok"})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "runner: streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-s.Local.Events():
			if !ok {
				return
			}
			raw, err := json.Marshal(ev)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, dest any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}
	if len(raw) > maxBody {
		err := errors.New("runner: body is too large")
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Listen serves the runner on a loopback address.
func Listen(addr, token string, local *Local) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("runner: token is empty")
	}
	if !loopbackAddr(addr) {
		return errors.New("runner: address must be loopback")
	}
	return ListenCode(addr, token, local, nil)
}

// ListenCode is Listen plus a handler for /v1/code/ routes.
func ListenCode(addr, token string, local *Local, code http.Handler) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("runner: token is empty")
	}
	if !loopbackAddr(addr) {
		return errors.New("runner: address must be loopback")
	}
	if local == nil {
		local = NewLocal(Limits{})
	}
	srv := &http.Server{Addr: addr, Handler: (&Server{Local: local, Token: token, Code: code}).Handler()}
	return srv.ListenAndServe()
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func loopbackRemote(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Remote is an Executor that talks to a runner over HTTP.
type Remote struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	// Active requests share their transcript directories with the loopback runner.
	active sync.Map // agent id -> directory; finished events remove it
	events chan Event
	once   sync.Once
	err    error
	ctx    context.Context
	cancel context.CancelFunc
}

// Dial returns a client. The event stream connects on the first Start or Control.
func Dial(baseURL, token string) *Remote {
	ctx, cancel := context.WithCancel(context.Background())
	return &Remote{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		events:  make(chan Event, 64),
		ctx:     ctx,
		cancel:  cancel,
		HTTP:    &http.Client{},
	}
}

// Events is the decoded server-sent event stream.
func (r *Remote) Events() <-chan Event { return r.events }

// Close stops the event stream.
func (r *Remote) Close() { r.cancel() }

// Start asks the runner to launch a subagent.
func (r *Remote) Start(ctx context.Context, req StartRequest) (Task, error) {
	if err := r.ensure(); err != nil {
		return Task{}, err
	}
	_, already := r.active.LoadOrStore(req.ID, req.Dir)
	var task Task
	if err := r.postResult(ctx, "/v1/start", req, &task); err != nil {
		if !already {
			r.active.Delete(req.ID)
		}
		return Task{}, err
	}
	return task, nil
}

// ActiveCount reads only live request metadata, never finished histories.
// Queued requests enter the count when the runner promotes them to running.
func (r *Remote) ActiveCount() int {
	n := 0
	r.active.Range(func(id, dir any) bool {
		m, err := ReadMeta(dir.(string))
		if err == nil {
			if m.Status == statusRunning {
				n++
			} else if m.Status != statusQueued {
				r.active.Delete(id)
			}
		}
		return true
	})
	return n
}

// Tasks uses the runner's admission snapshot; reading meta files cannot tell
// the current queue position. Keep only requests owned by this client.
func (r *Remote) Tasks() ([]Task, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.BaseURL+"/v1/tasks", nil)
	if err != nil {
		return nil, err
	}
	r.authorize(req)
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	resp, err := r.client().Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runner: tasks %s", resp.Status)
	}
	var list []Task
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return nil, err
	}
	out := list[:0]
	for _, task := range list {
		if _, ok := r.active.Load(task.ID); ok {
			out = append(out, task)
		}
	}
	return out, nil
}

// Control sends steer, queue, interrupt, or cancel.
func (r *Remote) Control(ctx context.Context, id string, c Control) error {
	if err := r.ensure(); err != nil {
		return err
	}
	c.ID = id
	return r.post(ctx, "/v1/message", c)
}

func (r *Remote) ensure() error {
	r.once.Do(func() {
		req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.BaseURL+"/v1/events", nil)
		if err != nil {
			r.err = err
			return
		}
		r.authorize(req)
		resp, err := r.client().Do(req)
		if err != nil {
			r.err = err
			return
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			r.err = fmt.Errorf("runner: events %s: %s", resp.Status, strings.TrimSpace(string(raw)))
			return
		}
		go r.read(resp.Body)
	})
	return r.err
}

func (r *Remote) read(body io.ReadCloser) {
	defer body.Close()
	defer close(r.events)
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			continue
		}
		if ev.Kind == EvDone || ev.Kind == EvFailed {
			r.active.Delete(ev.AgentID)
		}
		r.events <- ev
	}
}

func (r *Remote) post(ctx context.Context, path string, body any) error {
	return r.postResult(ctx, path, body, nil)
}

func (r *Remote) postResult(ctx context.Context, path string, body, result any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	r.authorize(req)
	resp, err := r.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("runner: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result)
	}
	return nil
}

func (r *Remote) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+r.Token)
}

func (r *Remote) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return http.DefaultClient
}
