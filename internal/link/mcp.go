package link

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/tools"
)

const maxMCPServers = 32

// providerKeyFn reports provider API key names. main sets it from config.HoldKeys.
// MCP must not copy those values into a child or an HTTP header.
var providerKeyFn func(string) bool

// SetProviderKey installs the check used before a secret env is passed on.
func SetProviderKey(fn func(string) bool) { providerKeyFn = fn }

func providerKey(name string) bool {
	if providerKeyFn == nil {
		return false
	}
	return providerKeyFn(strings.TrimSpace(name))
}

var (
	// ErrMCPExists means this bot already has a server with that name.
	ErrMCPExists = errors.New("mcp: already exists")
	// ErrMCPMissing means the bot has no server with that name.
	ErrMCPMissing = errors.New("mcp: not found")

	mcpNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// MCPServer is one MCP server for this bot. Command starts a stdio process.
// URL, when set, is a Streamable HTTP endpoint and Command is unused.
// SecretEnvs names variables; values stay out of the file. Provider API keys
// are rejected. Dial, when set, replaces both transports. Tests use it.
type MCPServer struct {
	Name       string                                                `json:"name"`
	Command    string                                                `json:"command,omitempty"`
	Args       []string                                              `json:"args"`
	URL        string                                                `json:"url,omitempty"`
	SecretEnvs []string                                              `json:"secret_envs"`
	Dial       func(ctx context.Context) (io.ReadWriteCloser, error) `json:"-"`
}

// MCP is one MCP server mounted as a Link. Name is mcp:<server>.
type MCP struct {
	Spec MCPServer

	once    sync.Once
	onceErr error

	mu     sync.Mutex
	cancel context.CancelFunc
	client *mcpClient
	listed bool
	tools  []tools.Tool
}

// NewMCP returns a link that connects on Tools or Run, not at construction.
func NewMCP(s MCPServer) *MCP {
	s.Args = nonNil(s.Args)
	s.SecretEnvs = nonNil(s.SecretEnvs)
	return &MCP{Spec: s}
}

func (m *MCP) Name() string { return "mcp:" + m.Spec.Name }

// Tools lists the server's tools. A connection failure yields no tools.
func (m *MCP) Tools() []tools.Tool {
	m.mu.Lock()
	if m.listed {
		out := m.tools
		m.mu.Unlock()
		return out
	}
	m.mu.Unlock()
	if err := m.connect(); err != nil {
		m.remember(nil)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpRPCWait)
	defer cancel()
	specs, err := m.client.listTools(ctx)
	if err != nil {
		m.remember(nil)
		return nil
	}
	return m.remember(m.build(specs))
}

func (m *MCP) remember(list []tools.Tool) []tools.Tool {
	if list == nil {
		list = []tools.Tool{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.listed {
		m.tools = list
		m.listed = true
	}
	return m.tools
}

// Run reads optional inbound notifications until ctx ends, then stops the process.
func (m *MCP) Run(ctx context.Context, emit func(Event) error) error {
	if err := m.connect(); err != nil {
		return err
	}
	defer m.Close()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case text, ok := <-m.client.notes:
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

// Close stops the server process. It is safe to call more than once.
func (m *MCP) Close() error {
	m.mu.Lock()
	cancel := m.cancel
	c := m.client
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c != nil {
		return c.close()
	}
	return nil
}

func (m *MCP) connect() error {
	m.once.Do(func() {
		m.onceErr = m.start()
	})
	return m.onceErr
}

func (m *MCP) start() error {
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := m.open(ctx)
	if err != nil {
		cancel()
		return err
	}
	client := newMCPClient(conn)
	m.mu.Lock()
	m.cancel = cancel
	m.client = client
	m.mu.Unlock()
	go client.readLoop()
	initCtx, initCancel := context.WithTimeout(context.Background(), mcpRPCWait)
	defer initCancel()
	if err := client.initialize(initCtx); err != nil {
		_ = m.Close()
		return err
	}
	return nil
}

func (m *MCP) open(ctx context.Context) (io.ReadWriteCloser, error) {
	if m.Spec.Dial != nil {
		return m.Spec.Dial(ctx)
	}
	if strings.TrimSpace(m.Spec.URL) != "" {
		return openStreamable(ctx, m.Spec)
	}
	if err := validateCommand(m.Spec.Command); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, m.Spec.Command, m.Spec.Args...)
	cmd.Env = spawnEnv(m.Spec.SecretEnvs, os.LookupEnv)
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return &stdioConn{Reader: stdout, w: stdin, c: stdout}, nil
}

func (m *MCP) build(specs []mcpToolSpec) []tools.Tool {
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
	if err := t.mcp.connect(); err != nil {
		return "", err
	}
	var arguments any
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "null" {
		arguments = map[string]any{}
	} else if err := json.Unmarshal(args, &arguments); err != nil {
		return "", err
	}
	raw, err := t.mcp.client.call(ctx, "tools/call", map[string]any{
		"name":      t.remote,
		"arguments": arguments,
	})
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

// LoadMCP reads servers from path. A missing file is an empty list.
func LoadMCP(path string) ([]MCPServer, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return []MCPServer{}, nil
	} else if err != nil {
		return nil, err
	}
	var out []MCPServer
	err := fsutil.WithLock(path+".lock", func() error {
		var err error
		out, err = readMCP(path)
		return err
	})
	return out, err
}

// LoadMCPAll reads several lists and merges them by name. A later path wins, so a
// bot's own entry overrides the owner-level one of the same name.
func LoadMCPAll(paths ...string) ([]MCPServer, error) {
	var out []MCPServer
	at := map[string]int{}
	for _, p := range paths {
		list, err := LoadMCP(p)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			if i, ok := at[s.Name]; ok {
				out[i] = s
				continue
			}
			at[s.Name] = len(out)
			out = append(out, s)
		}
	}
	if out == nil {
		out = []MCPServer{}
	}
	return out, nil
}

// AddMCP appends one server. A duplicate name returns ErrMCPExists.
func AddMCP(path string, s MCPServer) error {
	s = normalizeServer(s)
	if err := ValidateMCP(s); err != nil {
		return err
	}
	return fsutil.WithLock(path+".lock", func() error {
		list, err := readMCP(path)
		if err != nil {
			return err
		}
		for _, e := range list {
			if e.Name == s.Name {
				return ErrMCPExists
			}
		}
		if len(list) >= maxMCPServers {
			return errors.New("mcp: too many servers")
		}
		list = append(list, s)
		return writeMCP(path, list)
	})
}

// RemoveMCP deletes one server by name.
func RemoveMCP(path, name string) error {
	name = strings.TrimSpace(name)
	if !mcpNameRe.MatchString(name) {
		return ErrMCPMissing
	}
	return fsutil.WithLock(path+".lock", func() error {
		list, err := readMCP(path)
		if err != nil {
			return err
		}
		next := make([]MCPServer, 0, len(list))
		found := false
		for _, e := range list {
			if e.Name == name {
				found = true
				continue
			}
			next = append(next, e)
		}
		if !found {
			return ErrMCPMissing
		}
		return writeMCP(path, next)
	})
}

// ValidateMCP checks a server record before it is stored.
func ValidateMCP(s MCPServer) error {
	if !mcpNameRe.MatchString(s.Name) {
		return fmt.Errorf("mcp: invalid name %q", s.Name)
	}
	if err := validateEndpoint(s); err != nil {
		return err
	}
	if len(s.Args) > 64 {
		return errors.New("mcp: too many args")
	}
	for _, a := range s.Args {
		if strings.ContainsAny(a, "\n\r") || strings.ContainsRune(a, 0) {
			return errors.New("mcp: arg contains a newline")
		}
		if len(a) > 4096 {
			return errors.New("mcp: arg too long")
		}
	}
	if len(s.SecretEnvs) > 32 {
		return errors.New("mcp: too many secrets")
	}
	seen := map[string]bool{}
	for _, n := range s.SecretEnvs {
		if !envNameRe.MatchString(n) {
			return fmt.Errorf("mcp: invalid secret %q", n)
		}
		if providerKey(n) {
			return fmt.Errorf("mcp: provider key %q is not passed", n)
		}
		if seen[n] {
			return fmt.Errorf("mcp: duplicate secret %q", n)
		}
		seen[n] = true
	}
	return nil
}

func validateEndpoint(s MCPServer) error {
	hasURL := strings.TrimSpace(s.URL) != ""
	hasCmd := strings.TrimSpace(s.Command) != ""
	if hasURL && hasCmd {
		return errors.New("mcp: pick command or url")
	}
	if hasURL {
		return validateURL(s.URL)
	}
	return validateCommand(s.Command)
}

func validateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("mcp: empty url")
	}
	if len(raw) > 4096 || strings.ContainsAny(raw, "\n\r") || strings.ContainsRune(raw, 0) {
		return errors.New("mcp: invalid url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("mcp: url must be http or https")
	}
	return nil
}

func validateCommand(command string) error {
	if command == "" {
		return errors.New("mcp: empty command")
	}
	if len(command) > 4096 {
		return errors.New("mcp: command too long")
	}
	if strings.ContainsAny(command, "\n\r") || strings.ContainsRune(command, 0) {
		return errors.New("mcp: command contains a newline")
	}
	return nil
}

func normalizeServer(s MCPServer) MCPServer {
	s.Name = strings.TrimSpace(s.Name)
	s.Command = strings.TrimSpace(s.Command)
	s.URL = strings.TrimSpace(s.URL)
	s.Args = nonNil(s.Args)
	envs := make([]string, 0, len(s.SecretEnvs))
	for _, n := range s.SecretEnvs {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		envs = append(envs, n)
	}
	s.SecretEnvs = envs
	s.Dial = nil
	return s
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

type mcpFile struct {
	Servers []MCPServer `json:"servers"`
}

func readMCP(path string) ([]MCPServer, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []MCPServer{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return []MCPServer{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var file mcpFile
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	if file.Servers == nil {
		file.Servers = []MCPServer{}
	}
	if len(file.Servers) > maxMCPServers {
		return nil, errors.New("mcp: too many servers")
	}
	seen := map[string]bool{}
	for i, s := range file.Servers {
		s = normalizeServer(s)
		if err := ValidateMCP(s); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("mcp: duplicate %s", s.Name)
		}
		seen[s.Name] = true
		file.Servers[i] = s
	}
	return file.Servers, nil
}

func writeMCP(path string, list []MCPServer) error {
	if list == nil {
		list = []MCPServer{}
	}
	for i := range list {
		list[i] = normalizeServer(list[i])
	}
	raw, err := fsutil.Marshal(mcpFile{Servers: list})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}

// spawnEnv is the child environment: a short base list, plus named secrets that are already set.
func spawnEnv(names []string, lookup func(string) (string, bool)) []string {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var out []string
	have := map[string]bool{}
	add := func(k string) {
		if have[k] {
			return
		}
		v, ok := lookup(k)
		if !ok {
			return
		}
		have[k] = true
		out = append(out, k+"="+v)
	}
	for _, k := range []string{"PATH", "HOME", "TMPDIR", "LANG", "SYSTEMROOT"} {
		add(k)
	}
	for _, k := range names {
		if providerKey(k) {
			continue
		}
		add(k)
	}
	return out
}

type stdioConn struct {
	io.Reader
	w io.WriteCloser
	c io.Closer
}

func (s *stdioConn) Write(p []byte) (int, error) { return s.w.Write(p) }

func (s *stdioConn) Close() error {
	err1 := s.w.Close()
	err2 := s.c.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
