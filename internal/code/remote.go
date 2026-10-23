package code

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"xbot/internal/provider"
)

// Service runs coding tasks for xbot runner. It does not store the bot's record.
type Service struct {
	// Provider overrides the model on the start request. Tests use it.
	Provider provider.Provider
	mu       sync.Mutex
	live     map[string]*liveRun
	notes    map[string][]Note
	log      []remoteEvent
	notify   chan struct{}
}

type remoteEvent struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Text   string `json:"text"`
}

// NewService returns an empty runner-side coding service.
func NewService() *Service {
	return &Service{notify: make(chan struct{}, 1)}
}

// Handler is the loopback, bearer-token wrapper around the code routes.
func (s *Service) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopback(r.RemoteAddr) {
			http.Error(w, "runner: loopback only", http.StatusForbidden)
			return
		}
		if !bearer(r, token) {
			http.Error(w, "runner: unauthorized", http.StatusUnauthorized)
			return
		}
		s.ServeHTTP(w, r)
	})
}

// ServeHTTP handles /v1/code/start, /v1/code/message, and /v1/code/events.
// The runner's own auth wrapper calls this after it has checked the token.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/code/start":
		s.start(w, r)
	case "/v1/code/message":
		s.message(w, r)
	case "/v1/code/events":
		s.events(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Task
		Model Endpoint `json:"model"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validID(req.ID) {
		http.Error(w, "code: bad id", http.StatusBadRequest)
		return
	}
	p := s.Provider
	if p == nil && req.Model.BaseURL != "" && req.Model.APIKey != "" && req.Model.Model != "" {
		var err error
		p, err = provider.New(provider.Endpoint{Protocol: req.Model.Protocol, HostedSearch: req.Model.HostedSearch, BaseURL: req.Model.BaseURL, APIKey: req.Model.APIKey, Model: req.Model.Model, Thinking: req.Model.Thinking})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if p == nil {
		http.Error(w, "code: no model", http.StatusBadRequest)
		return
	}
	if err := validate(req.Task); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Status = StatusRunning
	s.mu.Lock()
	if s.live[req.ID] != nil {
		s.mu.Unlock()
		http.Error(w, "code: already running", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &liveRun{cancel: cancel}
	if s.live == nil {
		s.live = map[string]*liveRun{}
	}
	if s.notes == nil {
		s.notes = map[string][]Note{}
	}
	s.live[req.ID] = run
	s.mu.Unlock()
	go s.execute(ctx, run, req.Task, p)
	writeJSON(w, http.StatusOK, map[string]string{"id": req.ID, "status": StatusRunning})
}

func (s *Service) execute(ctx context.Context, run *liveRun, task Task, p provider.Provider) {
	defer run.cancel()
	defer func() {
		s.mu.Lock()
		delete(s.live, task.ID)
		delete(s.notes, task.ID)
		s.mu.Unlock()
	}()
	out := runModel(ctx, run, p, task, func() []Note {
		s.mu.Lock()
		defer s.mu.Unlock()
		return append([]Note(nil), s.notes[task.ID]...)
	})
	if ctx.Err() != nil && out.Status == StatusDone {
		out.Status = StatusCancelled
	}
	s.emit(remoteEvent{ID: task.ID, Status: out.Status, Text: out.Text})
}

func (s *Service) message(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := decodeJSON(r.Body, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	run := s.live[in.ID]
	if run == nil {
		s.mu.Unlock()
		http.Error(w, "code: not running", http.StatusNotFound)
		return
	}
	switch in.Kind {
	case KindSteer, KindQueue, KindInterrupt:
		s.notes[in.ID] = append(s.notes[in.ID], Note{Kind: in.Kind, Text: in.Text, At: time.Now().UTC()})
	case KindCancel:
	default:
		s.mu.Unlock()
		http.Error(w, "code: bad kind", http.StatusBadRequest)
		return
	}
	s.mu.Unlock()
	run.mu.Lock()
	if in.Kind == KindCancel && run.cancel != nil {
		run.cancel()
	}
	if in.Kind == KindInterrupt && run.stopStep != nil {
		run.stopStep()
	}
	run.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"id": in.ID, "status": "ok"})
}

func (s *Service) events(w http.ResponseWriter, r *http.Request) {
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
	sent := 0
	for {
		s.mu.Lock()
		batch := append([]remoteEvent(nil), s.log[sent:]...)
		s.mu.Unlock()
		for _, ev := range batch {
			raw, err := json.Marshal(ev)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return
			}
			flusher.Flush()
			sent++
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.notify:
		case <-time.After(15 * time.Second):
		}
	}
}

func (s *Service) emit(ev remoteEvent) {
	s.mu.Lock()
	s.log = append(s.log, ev)
	if len(s.log) > 256 {
		s.log = s.log[len(s.log)-256:]
	}
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func runRemote(ctx context.Context, base, token string, task Task, model Endpoint) outcome {
	body, err := json.Marshal(struct {
		Task
		Model Endpoint `json:"model,omitempty"`
	}{Task: task, Model: model})
	if err != nil {
		return outcome{Status: StatusFailed, Text: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/code/start", bytes.NewReader(body))
	if err != nil {
		return outcome{Status: StatusFailed, Text: scrubKey(err.Error(), model.APIKey)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return outcome{Status: StatusFailed, Text: scrubKey(err.Error(), model.APIKey)}
	}
	slurp, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return outcome{Status: StatusFailed, Text: scrubKey(fmt.Sprintf("runner: %d %s", res.StatusCode, slurp), model.APIKey)}
	}
	ev, err := waitEvent(ctx, base, token, task.ID, time.Duration(task.TimeoutSec)*time.Second+5*time.Second)
	if err != nil {
		return outcome{Status: StatusFailed, Text: scrubKey(err.Error(), model.APIKey)}
	}
	return outcome{Status: ev.Status, Text: ev.Text}
}

func waitEvent(ctx context.Context, base, token, id string, wait time.Duration) (remoteEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/code/events", nil)
	if err != nil {
		return remoteEvent{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return remoteEvent{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return remoteEvent{}, fmt.Errorf("runner: %d %s", res.StatusCode, slurp)
	}
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev remoteEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			return remoteEvent{}, err
		}
		if ev.ID == id && (ev.Status == StatusDone || ev.Status == StatusFailed || ev.Status == StatusCancelled) {
			return ev, nil
		}
	}
	if err := sc.Err(); err != nil {
		return remoteEvent{}, err
	}
	return remoteEvent{}, errors.New("runner: event stream closed")
}

func decodeJSON(rc io.Reader, dest any) error {
	raw, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bearer(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(got) != len(token) {
		return false
	}
	n := 0
	for i := 0; i < len(got); i++ {
		n |= int(got[i] ^ token[i])
	}
	return n == 0
}

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func scrubKey(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[secret]")
}
