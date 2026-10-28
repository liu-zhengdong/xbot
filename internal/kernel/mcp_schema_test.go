package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func schemaMCP(t *testing.T) *link.MCP {
	t.Helper()
	m := link.NewMCP(link.MCPServer{Name: "schema", Dial: func(context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			sc := bufio.NewScanner(server)
			for sc.Scan() {
				var in struct {
					ID     json.RawMessage
					Method string
				}
				if json.Unmarshal(sc.Bytes(), &in) != nil || len(in.ID) == 0 {
					continue
				}
				result := `{}`
				switch in.Method {
				case "initialize":
					result = `{"protocolVersion":"2024-11-05","capabilities":{}}`
				case "tools/list":
					result = `{"tools":[{"name":"bad","inputSchema":{"type":"object","properties":{"query":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}},{"name":"good","inputSchema":{"type":"object","properties":{}}}]}`
				}
				_, _ = fmt.Fprintf(server, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n", in.ID, result)
			}
		}()
		return client, nil
	}})
	t.Cleanup(func() { _ = m.Close() })
	return m
}

type schemaRejectProvider struct {
	reqs     []provider.Request
	rejected int
}

func (p *schemaRejectProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.reqs = append(p.reqs, snapshotReq(req))
	loaded := false
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser && !m.Internal {
			loaded = false
		}
		if m.Role == provider.RoleTool && m.Name == "links" {
			loaded = true
		}
	}
	if !loaded {
		return assistantCall(fmt.Sprintf("load%d", len(p.reqs)), "", "links", `{"action":"load","id":"mcp:schema"}`), nil
	}
	for _, td := range req.Tools {
		raw, _ := json.Marshal(td.Parameters)
		if td.Name == "mcp_schema_bad" && strings.Contains(string(raw), "oneOf") {
			p.rejected++
			return provider.Message{}, provider.NewStatusError(400, `{"error":{"message":"Invalid schema for function 'mcp_schema_bad': oneOf is not supported","code":"invalid_function_parameters"}}`)
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("conversation continues")}, nil
}

// The main line never declares an MCP schema, even after links load: MCP
// tools sit behind the tools door, so a vendor cannot reject them up front.
func TestMainLineNeverDeclaresMCPSchemas(t *testing.T) {
	b := testBot(t, link.NameTerminal, "mcp:schema")
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := link.AddMCP(b.MCPPath(), link.MCPServer{Name: "schema", Command: "fake-mcp"}); err != nil {
		t.Fatal(err)
	}
	p := &schemaRejectProvider{}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, Links: []link.Link{link.Terminal{}}, newMCP: func(link.MCPServer) link.Link { return schemaMCP(t) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "load the MCP tool", time.Now())); err != nil {
		t.Fatal(err)
	}
	if p.rejected != 0 || len(p.reqs) != 2 {
		t.Fatalf("rejections=%d requests=%d", p.rejected, len(p.reqs))
	}
	for _, req := range p.reqs {
		for _, td := range req.Tools {
			if strings.HasPrefix(td.Name, "mcp_") {
				t.Fatalf("MCP schema declared on the main line: %s", td.Name)
			}
		}
	}
	if _, ok := k.toolRegistry().find("mcp_schema_bad"); !ok {
		t.Fatal("loaded MCP tool is not registered")
	}
}

func schemaRequest() provider.Request {
	return provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: provider.TextContent("continue")}}, Tools: []provider.ToolDef{
		{Name: "mcp_schema_bad", Parameters: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}}}}},
		{Name: "mcp_schema_good", Parameters: map[string]any{"type": "object"}},
		{Name: "shell", Parameters: map[string]any{"type": "object"}},
	}}
}

func TestMCPSchemaFallbackBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            error
		calls, tools   int
		stream, cancel bool
	}{
		{"named", provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'"), 2, 2, false, false},
		{"unknown", provider.NewStatusError(400, "invalid tool schema"), 2, 1, false, false},
		{"builtin", provider.NewStatusError(400, "Invalid schema for function 'shell'"), 1, 3, false, false},
		{"not_offered", provider.NewStatusError(400, "Invalid schema for function 'mcp_hidden_tool'"), 1, 3, false, false},
		{"ordinary", provider.NewStatusError(400, "invalid request"), 1, 3, false, false},
		{"auth", provider.NewStatusError(401, "invalid tool schema"), 1, 3, false, false},
		{"overflow", provider.NewStatusError(400, "maximum context length"), 1, 3, false, false},
		{"stream", provider.NewStatusError(400, "invalid tool schema"), 1, 3, true, false},
		{"cancel", provider.NewStatusError(400, "invalid tool schema"), 1, 3, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &Kernel{Bot: testBot(t, link.NameTerminal)}
			attempt := &mcpSchemaAttempt{k: k}
			req := schemaRequest()
			before, _ := json.Marshal(req)
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sent, msg, err := attempt.chat(ctx, req, nil, func(_ context.Context, r provider.Request, delta func(string)) (provider.Message, error) {
				calls++
				if calls == 1 {
					if tc.stream {
						delta("partial")
					}
					if tc.cancel {
						cancel()
					}
					return provider.Message{}, tc.err
				}
				return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
			})
			if calls != tc.calls || len(sent.Tools) != tc.tools {
				t.Fatalf("calls=%d tools=%d", calls, len(sent.Tools))
			}
			if tc.calls == 1 && err != tc.err {
				t.Fatalf("lost original error: %v", err)
			}
			if tc.calls == 2 && (err != nil || msg.Text() != "ok") {
				t.Fatalf("retry: %v %v", msg, err)
			}
			after, _ := json.Marshal(req)
			if string(before) != string(after) {
				t.Fatal("mutated input request")
			}
		})
	}
}

func TestMCPSchemaRetryFailureIsBounded(t *testing.T) {
	for _, second := range []error{provider.NewStatusError(400, "invalid tool schema"), provider.NewStatusError(401, "unauthorized"), provider.NewStatusError(400, "another invalid request")} {
		k := &Kernel{Bot: testBot(t, link.NameTerminal)}
		attempt := &mcpSchemaAttempt{k: k}
		calls := 0
		_, _, err := attempt.chat(context.Background(), schemaRequest(), nil, func(context.Context, provider.Request, func(string)) (provider.Message, error) {
			calls++
			if calls == 1 {
				return provider.Message{}, provider.NewStatusError(400, "invalid tool schema")
			}
			return provider.Message{}, second
		})
		if calls != 2 || err != second {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
		// Even an unsuccessful recovery remembers the rejected candidates.
		again := (&mcpSchemaAttempt{k: k}).filter(schemaRequest())
		if len(again.Tools) != 1 {
			t.Fatalf("forgot cooldown after failure: %d tools", len(again.Tools))
		}
	}
}

func TestMCPSchemaRecoveryAndIsolation(t *testing.T) {
	now := time.Now()
	b := testBot(t, link.NameTerminal)
	if err := b.SetBinding("fake", "one"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Now: func() time.Time { return now }}
	a := &mcpSchemaAttempt{k: k}
	req := schemaRequest()
	if !a.reject(req, provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'")) {
		t.Fatal("not rejected")
	}
	// Cooldown is stable under repeated load/rejection, not renewed each turn.
	for i := 0; i < 3; i++ {
		other := &mcpSchemaAttempt{k: &Kernel{Bot: b, Now: k.Now}}
		if len(other.filter(req).Tools) != 2 {
			t.Fatal("lost across kernels")
		}
		other.reject(req, provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'"))
	}
	changed := schemaRequest()
	changed.Tools[0].Parameters = map[string]any{"type": "object"}
	if len(a.filter(changed).Tools) != 3 {
		t.Fatal("new schema did not recover")
	}
	if err := b.SetBinding("fake", "two"); err != nil {
		t.Fatal(err)
	}
	if len(a.filter(req).Tools) != 3 {
		t.Fatal("new model did not recover")
	}
	if err := b.SetBinding("fake", "one"); err != nil {
		t.Fatal(err)
	}
	foreign := &mcpSchemaAttempt{k: &Kernel{Bot: testBot(t, link.NameTerminal)}}
	if len(foreign.filter(req).Tools) != 3 {
		t.Fatal("cooldown leaked to another bot")
	}
	now = now.Add(mcpSchemaCooldown)
	if len(a.filter(req).Tools) != 3 {
		t.Fatal("cooldown did not expire")
	}
}

func TestMCPSchemaNotesDoNotExposeUpstream(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	a := &mcpSchemaAttempt{k: k}
	req := schemaRequest()
	if !a.reject(req, provider.NewStatusError(400, "invalid tool schema: upstream-private-marker <untrusted>")) {
		t.Fatal("not rejected")
	}
	got := a.filter(req)
	raw, _ := json.Marshal(got)
	traces, err := k.Bot.Traces()
	if err != nil {
		t.Fatal(err)
	}
	log, _ := json.Marshal(traces)
	if strings.Contains(string(raw)+string(log), "upstream-private-marker") {
		t.Fatal("upstream body leaked")
	}
	if len(traces) != 1 || traces[0].Kind != "mcp_schema" || traces[0].Reason != "tools_suspended" {
		t.Fatalf("missing trace: %v", traces)
	}
	// No requested MCP tools means no cache-derived resource disclosure.
	req.Tools = req.Tools[2:]
	if len(a.filter(req).Messages) != 1 {
		t.Fatal("hidden tools disclosed")
	}
}

func TestMCPSchemaSubagentFallback(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	calls := 0
	p := &schemaFuncProvider{chat: func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		calls++
		for _, td := range req.Tools {
			if td.Name == "mcp_schema_bad" {
				return provider.Message{}, provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'")
			}
		}
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
	}}
	fitted := &fitProvider{inner: p, k: k, fit: newTurnFit(1, nil)}
	for i := 0; i < 2; i++ {
		msg, err := fitted.Chat(context.Background(), schemaRequest(), nil)
		if err != nil || msg.Text() != "ok" {
			t.Fatalf("%v %v", msg, err)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

type schemaFuncProvider struct {
	chat func(context.Context, provider.Request, func(string)) (provider.Message, error)
}

func (p *schemaFuncProvider) Chat(ctx context.Context, r provider.Request, d func(string)) (provider.Message, error) {
	return p.chat(ctx, r, d)
}

func TestMCPSchemaConcurrentCooldown(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	req := schemaRequest()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				a := &mcpSchemaAttempt{k: k}
				a.reject(req, provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'"))
				if got := a.filter(req); len(got.Tools) != 2 {
					t.Errorf("tools=%d", len(got.Tools))
				}
			}
		}()
	}
	wg.Wait()
	if len(req.Tools) != 3 || len(req.Messages) != 1 {
		t.Fatal("shared request mutated")
	}
}

func TestMCPSchemaBounds(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	a := &mcpSchemaAttempt{k: k}
	for _, td := range []provider.ToolDef{
		{Name: "mcp_" + strings.Repeat("a", 61), Parameters: map[string]any{}},
		{Name: "mcp_unsafe\nname", Parameters: map[string]any{}},
		{Name: "mcp_unsafe", Parameters: make(chan int)},
		{Name: "mcp_large", Parameters: strings.Repeat("a", 1<<20)},
	} {
		if _, ok := a.key(td); ok {
			t.Fatalf("unsafe key accepted: %q", td.Name)
		}
	}
	req := schemaRequest()
	req.Tools = nil
	for i := 0; i < 100; i++ {
		req.Tools = append(req.Tools, provider.ToolDef{Name: fmt.Sprintf("mcp_tool_%03d", i), Parameters: map[string]any{"type": "object"}})
	}
	if !a.reject(req, provider.NewStatusError(400, "invalid tool schema")) {
		t.Fatal("not rejected")
	}
	got := a.filter(req)
	if len(got.Tools) != 0 || len(got.Messages) != 2 || len(got.Messages[1].Text()) > 4096 {
		t.Fatal("unbounded notice or missing fallback")
	}
	// Fill the process cache with synthetic old entries, then require eviction
	// to preserve the hard cap while allowing a newly rejected schema.
	rejectedMCPSchemas.Lock()
	saved := rejectedMCPSchemas.entries
	rejectedMCPSchemas.entries = make(map[[32]byte]time.Time)
	for i := 0; i < maxRejectedMCPSchemas; i++ {
		var key [32]byte
		key[0] = byte(i)
		key[1] = byte(i >> 8)
		rejectedMCPSchemas.entries[key] = time.Now().Add(time.Minute)
	}
	rejectedMCPSchemas.Unlock()
	defer func() { rejectedMCPSchemas.Lock(); rejectedMCPSchemas.entries = saved; rejectedMCPSchemas.Unlock() }()
	a = &mcpSchemaAttempt{k: k}
	if !a.reject(schemaRequest(), provider.NewStatusError(400, "invalid tool schema")) {
		t.Fatal("not rejected at capacity")
	}
	rejectedMCPSchemas.Lock()
	n := len(rejectedMCPSchemas.entries)
	rejectedMCPSchemas.Unlock()
	if n != maxRejectedMCPSchemas || len(a.filter(schemaRequest()).Tools) != 1 {
		t.Fatalf("cache size=%d", n)
	}
}

func TestMCPSchemaAndOverflowRetriesStaySeparate(t *testing.T) {
	for _, order := range []string{"schema-first", "overflow-first"} {
		t.Run(order, func(t *testing.T) {
			k := &Kernel{Bot: testBot(t, link.NameTerminal)}
			calls, overflow := 0, 0
			p := &schemaFuncProvider{chat: func(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
				calls++
				if calls == 3 {
					return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
				}
				if (calls == 1) == (order == "schema-first") {
					return provider.Message{}, provider.NewStatusError(400, "Invalid schema for function 'mcp_schema_bad'")
				}
				return provider.Message{}, provider.NewStatusError(400, "maximum context length exceeded")
			}}
			rp := &replyProvider{inner: p, schemaKernel: k, fit: func(_ context.Context, r provider.Request, o bool) provider.Request {
				if o {
					overflow++
				}
				return r
			}}
			msg, err := rp.Chat(context.Background(), schemaRequest(), nil)
			if err != nil || msg.Text() != "ok" || calls != 3 || overflow != 1 {
				t.Fatalf("msg=%v err=%v calls=%d overflow=%d", msg, err, calls, overflow)
			}
		})
	}
}

func TestMCPSchemaFilterLatency(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTerminal)}
	a := &mcpSchemaAttempt{k: k}
	req := schemaRequest()
	req.Tools = nil
	for i := 0; i < 100; i++ {
		req.Tools = append(req.Tools, provider.ToolDef{Name: fmt.Sprintf("mcp_tool_%03d", i), Parameters: map[string]any{"type": "object", "description": strings.Repeat("x", 1000)}})
	}
	a.reject(req, provider.NewStatusError(400, "invalid tool schema"))
	samples := make([]time.Duration, 200)
	for i := range samples {
		start := time.Now()
		a.filter(req)
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("cached filter: 100 MCP tools, ~1 KiB/schema, 200 samples, p95=%s", samples[len(samples)*95/100])
}
