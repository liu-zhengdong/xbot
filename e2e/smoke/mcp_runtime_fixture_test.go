//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Independent Streamable HTTP peer. All product calls still cross the real
// binary, MCP transport and current tool door. This is not a product hook.
type mcpRuntimePeer struct {
	server   *httptest.Server
	mu       sync.Mutex
	sessions map[string]*mcpRuntimeSession
	calls    map[string][]string
	gates    map[string]*mcpRuntimeGate
	stopping chan struct{}
}

type mcpRuntimeSession struct {
	id, path string
	count    int
	notes    chan string
	closed   chan struct{}
}

type mcpRuntimeGate struct {
	entered, release, canceled, finished chan struct{}
	once                                 sync.Once
	minimum                              time.Duration
	endpoint                             string
	started                              time.Time
}

func newMCPRuntimePeer(t *testing.T) *mcpRuntimePeer {
	p := &mcpRuntimePeer{sessions: map[string]*mcpRuntimeSession{}, calls: map[string][]string{}, gates: map[string]*mcpRuntimeGate{}, stopping: make(chan struct{})}
	p.server = httptest.NewServer(http.HandlerFunc(p.handle))
	t.Cleanup(func() { close(p.stopping); p.server.Close() })
	return p
}

func (p *mcpRuntimePeer) gate(label string, minimum time.Duration) *mcpRuntimeGate {
	p.mu.Lock()
	defer p.mu.Unlock()
	g := &mcpRuntimeGate{entered: make(chan struct{}), release: make(chan struct{}), canceled: make(chan struct{}), finished: make(chan struct{}), minimum: minimum}
	p.gates[label] = g
	return g
}

func (g *mcpRuntimeGate) allow() { g.once.Do(func() { close(g.release) }) }

func (p *mcpRuntimePeer) waitGate(w http.ResponseWriter, r *http.Request, g *mcpRuntimeGate) bool {
	defer close(g.finished)
	p.mu.Lock()
	g.started, g.endpoint = time.Now(), r.URL.Query().Get("endpoint")
	p.mu.Unlock()
	close(g.entered)
	timer := time.NewTimer(g.minimum)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
		close(g.canceled)
		return false
	case <-p.stopping:
		return false
	}
	select {
	case <-g.release:
		return true
	case <-r.Context().Done():
		close(g.canceled)
		return false
	case <-p.stopping:
		return false
	}
}

func (p *mcpRuntimePeer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/gate/") {
		p.mu.Lock()
		g := p.gates[strings.TrimPrefix(r.URL.Path, "/gate/")]
		p.mu.Unlock()
		if g == nil {
			http.NotFound(w, r)
			return
		}
		if p.waitGate(w, r, g) {
			_, _ = io.WriteString(w, "independent gate released\n")
		}
		return
	}
	id := r.Header.Get("Mcp-Session-Id")
	p.mu.Lock()
	session := p.sessions[id]
	p.mu.Unlock()
	if r.Method == http.MethodGet {
		if session == nil {
			http.Error(w, "unknown session", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		defer close(session.closed)
		for {
			select {
			case note := <-session.notes:
				payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]string{"text": note}})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			case <-p.stopping:
				return
			}
		}
	}
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string `json:"name"`
			Arguments struct {
				Label string `json:"label"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&msg); err != nil {
		http.Error(w, "invalid rpc", 400)
		return
	}
	if strings.HasPrefix(msg.Method, "notifications/") {
		w.WriteHeader(202)
		return
	}
	var result any
	switch msg.Method {
	case "initialize":
		p.mu.Lock()
		id = fmt.Sprintf("%s-%d", strings.Trim(r.URL.Path, "/"), len(p.sessions)+1)
		session = &mcpRuntimeSession{id: id, path: r.URL.Path, notes: make(chan string, 16), closed: make(chan struct{})}
		p.sessions[id] = session
		p.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "independent-runtime", "version": "1"}}
	case "tools/list":
		result = map[string]any{"tools": []any{
			map[string]any{"name": "counter", "description": "runtime-counter", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]string{"type": "string"}}}},
			map[string]any{"name": "held", "description": "runtime-held", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]string{"type": "string"}}}},
		}}
	case "tools/call":
		if session == nil {
			http.Error(w, "unknown session", 400)
			return
		}
		label := msg.Params.Arguments.Label
		p.mu.Lock()
		session.count++
		count := session.count
		p.calls[label] = append(p.calls[label], id)
		g := p.gates[label]
		p.mu.Unlock()
		if g != nil && !p.waitGate(w, r, g) {
			return
		}
		payload, _ := json.Marshal(map[string]any{"session": id, "count": count, "label": label})
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "RUNTIME_RESULT " + string(payload)}}}
	default:
		http.Error(w, "unexpected rpc", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
}

func (p *mcpRuntimePeer) sessionCount() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sessions) }
func (p *mcpRuntimePeer) note(t *testing.T, id, text string) {
	t.Helper()
	p.mu.Lock()
	s := p.sessions[id]
	p.mu.Unlock()
	if s == nil {
		t.Fatalf("no session %q", id)
	}
	s.notes <- text
}
func (p *mcpRuntimePeer) callCount(label string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls[label])
}

type mcpRuntimeStep func([]string) (string, any)
type mcpRuntimeScript struct {
	steps   []mcpRuntimeStep
	results []string
	request string
}
type mcpRuntimeModel struct {
	server  *httptest.Server
	mu      sync.Mutex
	scripts map[string]*mcpRuntimeScript
}

func newMCPRuntimeModel(t *testing.T) *mcpRuntimeModel {
	m := &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}
func (m *mcpRuntimeModel) add(cue string, steps ...mcpRuntimeStep) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scripts[cue] = &mcpRuntimeScript{steps: steps, results: make([]string, len(steps))}
}
func (m *mcpRuntimeModel) results(cue string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.scripts[cue].results...)
}
func (m *mcpRuntimeModel) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req stubRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "bad scripted request", 400)
		return
	}
	caller, _ := classify(req)
	if caller == callerCompaction || caller == callerExtract {
		streamText(w, []string{"No durable facts."})
		return
	}
	var wire struct {
		Messages []struct {
			Role, Content string
			ToolCallID    string `json:"tool_call_id"`
		}
	}
	_ = json.Unmarshal(raw, &wire)
	m.mu.Lock()
	for cue, script := range m.scripts {
		for i := range script.results {
			for _, msg := range wire.Messages {
				if msg.Role == "tool" && msg.ToolCallID == fmt.Sprintf("%s_%d", cue, i) {
					script.results[i] = msg.Content
				}
			}
		}
	}
	var cue string
	for candidate := range m.scripts {
		if strings.Contains(lastUser(req), candidate) {
			cue = candidate
			break
		}
	}
	script := m.scripts[cue]
	if script == nil {
		m.mu.Unlock()
		streamOwner(w, req, "Background report received.")
		return
	}
	if script.request == "" {
		script.request = lastUser(req)
	}
	step := 0
	for step < len(script.results) && script.results[step] != "" {
		step++
	}
	results := append([]string(nil), script.results...)
	var next mcpRuntimeStep
	if step < len(script.steps) {
		next = script.steps[step]
	}
	m.mu.Unlock()
	if next != nil {
		name, args := next(results)
		streamTool(w, fmt.Sprintf("%s_%d", cue, step), name, argsJSON(args))
		return
	}
	if sawTool(req, "send_message_to_user") {
		streamText(w, []string{cue + "_FINISHED"})
		return
	}
	streamOwner(w, req, cue+" reply")
}

func mcpRuntimeTool(name string, args any) mcpRuntimeStep {
	return func([]string) (string, any) { return name, args }
}

// Only this adapter bridge knows today's door shape. The local MCP tool id is
// discovered from describe, not pinned as a permanent name/schema requirement.
func mcpRuntimeCall(kind, label string) []mcpRuntimeStep {
	return []mcpRuntimeStep{
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "mcp:runtime-fixture"}),
		func(results []string) (string, any) {
			var group struct {
				Tools []struct{ ID, Summary string }
			}
			text := results[len(results)-2]
			for i := len(results) - 1; i >= 0; i-- {
				if strings.Contains(results[i], `"tools"`) {
					text = results[i]
					break
				}
			}
			start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
			if start >= 0 && end > start {
				_ = json.Unmarshal([]byte(text[start:end+1]), &group)
			}
			id := "discovery-did-not-return-fixture-tool"
			for _, member := range group.Tools {
				if strings.Contains(member.Summary, "runtime-"+kind) {
					id = member.ID
				}
			}
			return "tools", map[string]any{"action": "call", "id": id, "args": map[string]string{"label": label}}
		},
	}
}
