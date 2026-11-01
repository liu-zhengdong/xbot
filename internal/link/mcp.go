package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"xbot/internal/mcp"
	"xbot/internal/tools"
)

// MCPServer is the transport configuration shared with the MCP core.
type MCPServer = mcp.Spec

// MCPStatus describes this instance without connecting it.
type MCPStatus = mcp.Status

var (
	ErrMCPExists  = mcp.ErrMCPExists
	ErrMCPMissing = mcp.ErrMCPMissing
)

// providerKeyFn remains the dynamically installed product credential predicate.
var providerKeyFn func(string) bool

func SetProviderKey(fn func(string) bool) { providerKeyFn = fn }
func providerKey(name string) bool {
	if providerKeyFn == nil {
		return false
	}
	return providerKeyFn(strings.TrimSpace(name))
}
func LoadMCP(path string) ([]MCPServer, error) {
	return mcp.LoadMCP(path, mcp.Options{IsProviderKey: providerKey})
}
func LoadMCPAll(paths ...string) ([]MCPServer, error) {
	return mcp.LoadMCPAll(mcp.Options{IsProviderKey: providerKey}, paths...)
}
func AddMCP(path string, s MCPServer) error {
	return mcp.AddMCP(path, s, mcp.Options{IsProviderKey: providerKey})
}
func ReplaceMCP(path string, s MCPServer, stale func(MCPServer) bool) error {
	return mcp.ReplaceMCP(path, s, stale, mcp.Options{IsProviderKey: providerKey})
}
func RemoveMCP(path, name string) error {
	return mcp.RemoveMCP(path, name, mcp.Options{IsProviderKey: providerKey})
}
func ValidateMCP(s MCPServer) error {
	return mcp.ValidateMCP(s, mcp.Options{IsProviderKey: providerKey})
}

// MCP adapts one owned core Session to the Link tool and event interfaces.
type MCP struct {
	Spec    MCPServer
	session *mcp.Session
	ref     *mcp.BotRef
}

// NewMCP returns a link that connects on Tools or Run, not at construction.
func NewMCP(s MCPServer) *MCP {
	session := mcp.NewSession(s, mcp.Options{IsProviderKey: providerKey})
	return &MCP{Spec: session.Spec, session: session}
}

// NewBorrowedMCP adapts the host's existing session without owning its lifetime
// or consuming notifications. The Manager owns the sole notification pump.
func NewBorrowedMCP(spec MCPServer, ref *mcp.BotRef) *MCP {
	return &MCP{Spec: spec, ref: ref}
}

func (m *MCP) Borrowed() bool { return m.ref != nil }

func (m *MCP) Name() string { return "mcp:" + m.Spec.Name }

// Owns says a tool name follows this server's naming, so a caller can start
// the right server for a name it saw in an earlier turn.
func (m *MCP) Owns(tool string) bool {
	return strings.HasPrefix(tool, "mcp_"+sanitizeTool(m.Spec.Name)+"_")
}

// Tools preserves the Link interface; failures remain observable through Status.
// Call LoadTools when the caller can return an error (notably links load).
func (m *MCP) Tools() []tools.Tool {
	list, _ := m.LoadTools(context.Background())
	return list
}

// LoadTools projects the core protocol specs into model tools.
func (m *MCP) LoadTools(ctx context.Context) ([]tools.Tool, error) {
	var specs []mcp.ToolSpec
	var err error
	if m.ref != nil {
		specs, err = m.ref.LoadTools(ctx, m.Spec.Name)
	} else {
		specs, err = m.session.LoadTools(ctx)
	}
	if err != nil {
		return nil, err
	}
	return m.build(specs), nil
}

func (m *MCP) Status() MCPStatus {
	if m.ref != nil {
		return m.ref.Status(m.Spec.Name)
	}
	return m.session.Status()
}
func (m *MCP) Close() error {
	if m.ref != nil {
		return nil
	}
	return m.session.Close()
}

// Run reads optional inbound notifications until ctx ends, then stops the process.
func (m *MCP) Run(ctx context.Context, emit func(Event) error) error {
	if m.ref != nil {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := m.session.Connect(context.Background()); err != nil {
		return err
	}
	defer m.Close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case text, ok := <-m.session.Notifications():
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return errors.New("mcp: server closed")
			}
			ev := NewEvent("mcp:"+m.Spec.Name, TypeMessage, text, "mcp:"+m.Spec.Name, time.Now())
			if err := emit(ev); err != nil {
				return err
			}
		}
	}
}

func (m *MCP) build(specs []mcp.ToolSpec) []tools.Tool {
	used := map[string]bool{}
	out := make([]tools.Tool, 0, len(specs))
	for _, spec := range specs {
		remote := strings.TrimSpace(spec.Name)
		if remote == "" {
			continue
		}
		desc := strings.TrimSpace(spec.Description)
		if desc == "" {
			desc = remote
		}
		out = append(out, mcpTool{
			mcp:      m,
			expected: m.Spec,
			remote:   remote,
			local:    localToolName(m.Spec.Name, remote, used),
			desc:     desc,
			schema:   schemaOrEmpty(spec.InputSchema),
			readOnly: spec.Annotations.ReadOnlyHint,
		})
	}
	return out
}

type mcpTool struct {
	mcp      *MCP
	expected MCPServer
	remote   string
	local    string
	desc     string
	schema   any
	readOnly bool
}

func (t mcpTool) Name() string { return t.local }

// ReadOnlyHint reports the server's readOnlyHint annotation. The kernel
// review table classifies such MCP tools as readonly.
func (t mcpTool) ReadOnlyHint() bool  { return t.readOnly }
func (t mcpTool) Description() string { return t.desc }
func (t mcpTool) Parameters() any     { return t.schema }

// Confirm only builds the card summary. The kernel review path decides:
// MCP tools are an external send unless the server marks them readOnlyHint.
func (t mcpTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "mcp " + t.mcp.Spec.Name + " " + t.remote, nil
}

func (t mcpTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if t.mcp.ref == nil {
		if err := t.mcp.session.Connect(context.Background()); err != nil {
			return "", err
		}
	}
	var arguments any
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "null" {
		arguments = map[string]any{}
	} else if err := json.Unmarshal(args, &arguments); err != nil {
		return "", err
	}
	var raw json.RawMessage
	var err error
	if t.mcp.ref != nil {
		raw, err = t.mcp.ref.CallToolForSpec(ctx, t.expected, t.remote, arguments)
	} else {
		raw, err = t.mcp.session.CallTool(ctx, t.remote, arguments)
	}
	if err != nil {
		return "", err
	}
	return parseToolResult(raw)
}

func parseToolResult(raw json.RawMessage) (string, error) {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if len(strings.TrimSpace(string(raw))) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &res); err != nil {
			return "", err
		}
	}
	var parts []string
	for _, c := range res.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if res.IsError {
		if text == "" {
			text = "tool failed"
		}
		return "", fmt.Errorf("mcp: %s", text)
	}
	return text, nil
}

func schemaOrEmpty(raw json.RawMessage) any {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil || v == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return v
}

func localToolName(server, remote string, used map[string]bool) string {
	base := fitTool("mcp_" + sanitizeTool(server) + "_" + sanitizeTool(remote))
	if !used[base] {
		used[base] = true
		return base
	}
	for i := 2; i < 100; i++ {
		suffix := "_" + strconv.Itoa(i)
		n := base
		if len(n)+len(suffix) > 64 {
			n = n[:64-len(suffix)]
		}
		n += suffix
		if !used[n] {
			used[n] = true
			return n
		}
	}
	return base
}

func sanitizeTool(s string) string {
	if s == "" {
		return "tool"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}

func fitTool(s string) string {
	if len(s) <= 64 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	tail := hex.EncodeToString(sum[:4])
	return s[:64-len(tail)-1] + "_" + tail
}
