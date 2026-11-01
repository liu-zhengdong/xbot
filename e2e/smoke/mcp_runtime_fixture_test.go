//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// Independent Streamable HTTP peer. All product calls still cross the real
// binary, source Shell/CLI and MCP transport. This is not a product hook.
type mcpRuntimePeer struct {
	server    *httptest.Server
	mu        sync.Mutex
	sessions  map[string]*mcpRuntimeSession
	calls     map[string][]string
	arguments map[string]json.RawMessage
	image     []byte
	fetches   int
	callRPCs  int
	gates     map[string]*mcpRuntimeGate
	stopping  chan struct{}
}

type mcpRuntimeSession struct {
	id, path   string
	credential string // fixture-only; never serialized or printed
	count      int
	notes      chan string
	closed     chan struct{}
}

type mcpRuntimeGate struct {
	entered, release, canceled, finished    chan struct{}
	once, enterOnce, cancelOnce, finishOnce sync.Once
	minimum                                 time.Duration
	endpoint                                string
	started                                 time.Time
}

func newMCPRuntimePeer(t *testing.T) *mcpRuntimePeer {
	p := &mcpRuntimePeer{sessions: map[string]*mcpRuntimeSession{}, calls: map[string][]string{}, arguments: map[string]json.RawMessage{}, gates: map[string]*mcpRuntimeGate{}, stopping: make(chan struct{})}
	img := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.NRGBA{R: 12, G: 120, B: 230, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	p.image = encoded.Bytes()
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
	defer g.finishOnce.Do(func() { close(g.finished) })
	g.enterOnce.Do(func() {
		p.mu.Lock()
		g.started, g.endpoint = time.Now(), r.URL.Query().Get("endpoint")
		p.mu.Unlock()
		close(g.entered)
	})
	timer := time.NewTimer(g.minimum)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
		g.cancelOnce.Do(func() { close(g.canceled) })
		return false
	case <-p.stopping:
		return false
	}
	select {
	case <-g.release:
		return true
	case <-r.Context().Done():
		g.cancelOnce.Do(func() { close(g.canceled) })
		return false
	case <-p.stopping:
		return false
	}
}

func (p *mcpRuntimePeer) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/resource-must-not-fetch" {
		p.mu.Lock()
		p.fetches++
		p.mu.Unlock()
		http.Error(w, "resource links must remain references", 400)
		return
	}
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
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
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
		session = &mcpRuntimeSession{id: id, path: r.URL.Path, credential: r.Header.Get("MCP_RUNTIME_TOKEN"), notes: make(chan string, 16), closed: make(chan struct{})}
		p.sessions[id] = session
		p.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "independent-runtime", "version": "1"}}
	case "tools/list":
		p.mu.Lock()
		listGate := p.gates["readonly-list"]
		p.mu.Unlock()
		if r.URL.Path == "/hint-check" && listGate != nil && !p.waitGate(w, r, listGate) {
			return
		}
		var definitions []any
		for _, name := range []string{"counter", "held", mcpRuntimeRawName, "mixed-result", "remote-error", "bad-image"} {
			description := "runtime-" + name
			if name == mcpRuntimeRawName && r.URL.Path == "/large-tools" {
				description = strings.Repeat("COMPLETE_SCHEMA_", 6000)
			}
			definitions = append(definitions, map[string]any{"name": name, "description": description,
				"inputSchema":        map[string]any{"type": "object", "x-independent-schema": "retained", "properties": map[string]any{"label": map[string]string{"type": "string"}}},
				"annotations":        map[string]any{"readOnlyHint": name != "held" && r.URL.Path != "/hint-replacement", "x-independent-hint": "retained"},
				"x-independent-spec": "retained"})
		}
		result = map[string]any{"tools": definitions}
	case "tools/call":
		p.mu.Lock()
		p.callRPCs++
		p.mu.Unlock()
		if session == nil {
			http.Error(w, "unknown session", 400)
			return
		}
		switch msg.Params.Name {
		case "counter", "held", mcpRuntimeRawName, "mixed-result", "remote-error", "bad-image":
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "exact remote tool name required"}})
			return
		}
		var args struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(msg.Params.Arguments, &args); err != nil {
			http.Error(w, "invalid arguments", 400)
			return
		}
		label := args.Label
		p.mu.Lock()
		session.count++
		count := session.count
		p.calls[label] = append(p.calls[label], id)
		p.arguments[label] = append(json.RawMessage(nil), msg.Params.Arguments...)
		g := p.gates[label]
		p.mu.Unlock()
		if g != nil && !p.waitGate(w, r, g) {
			return
		}
		payload, _ := json.Marshal(map[string]any{"session": id, "count": count, "label": label})
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "RUNTIME_RESULT " + string(payload)}}}
		switch msg.Params.Name {
		case "remote-error":
			result = map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": "REMOTE_ERROR_CONTENT_RETAINED"}}}
		case "bad-image":
			result = map[string]any{"content": []any{map[string]string{"type": "image", "mimeType": "image/png", "data": "not-valid-base64"}}}
		case "mixed-result":
			result = map[string]any{"content": []any{
				map[string]string{"type": "text", "text": strings.Repeat("COMPLETE_TEXT_", 8000)},
				map[string]string{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(p.image)},
				map[string]any{"type": "independent-unknown", "vendor": map[string]string{"complete": "UNKNOWN_BLOCK_RETAINED"}},
				map[string]string{"type": "resource_link", "uri": p.server.URL + "/resource-must-not-fetch"},
			}, "structuredContent": map[string]any{"complete": "STRUCTURED_RETAINED", "number": json.Number("9007199254740993")}, "x-independent-raw": "RAW_EXTENSION_RETAINED"}
		}
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
func (p *mcpRuntimePeer) session(id string) *mcpRuntimeSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessions[id]
}
func (p *mcpRuntimePeer) firstSession() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.sessions {
		return id
	}
	return ""
}
func (p *mcpRuntimePeer) rpcCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.callRPCs }
func (p *mcpRuntimePeer) callCount(label string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls[label])
}

type mcpRuntimeStep func([]string) (string, any)
type mcpRuntimeScript struct {
	steps     []mcpRuntimeStep
	results   []string
	request   string
	callers   map[string]bool
	declared  map[string]bool
	mcpSchema bool
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
	m.scripts[cue] = &mcpRuntimeScript{steps: steps, results: make([]string, len(steps)), callers: map[string]bool{}, declared: map[string]bool{}}
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
		Tools []struct {
			Function struct {
				Name       string
				Parameters json.RawMessage
			}
		}
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
	script.callers[caller] = true
	for _, tool := range wire.Tools {
		script.declared[tool.Function.Name] = true
		if bytes.Contains(tool.Function.Parameters, []byte("x-independent-schema")) {
			script.mcpSchema = true
		}
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
	if caller == callerSubagent {
		streamText(w, []string{cue + "_FINISHED"})
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

const mcpRuntimeRawName = "counter/remote.v1"

// 2b has one real Shell/CLI driver. No old MCP model tool name or door remains.
func mcpRuntimeShell(command string) mcpRuntimeStep {
	return mcpRuntimeTool("sh", map[string]string{"description": "Use the current runtime MCP CLI", "command": command})
}
func mcpRuntimeCLI(arguments string) mcpRuntimeStep {
	return mcpRuntimeShell(`"$XBOT_BIN" mcp ` + arguments + ` --self --json`)
}
func mcpRuntimeCall(kind, label string) []mcpRuntimeStep {
	args, _ := json.Marshal(map[string]string{"label": label})
	return []mcpRuntimeStep{mcpRuntimeCLI("call runtime-fixture " + shellQuote(kind) + " --args " + shellQuote(string(args)))}
}

// External human CLI checks need distinct stdout/stderr and real stdin. Long
// calls go through source Shell instead, with the product's signal context.
func mcpRuntimeExternal(t *testing.T, env []string, stdin string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, xbotBinary, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("bounded external CLI did not return: %v", args)
	}
	return stdout.String(), stderr.String(), err
}
