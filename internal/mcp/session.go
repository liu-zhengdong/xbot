package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Options supplies the provider credential check shared by config and transports.
type Options struct {
	IsProviderKey func(string) bool
}

// Session owns one lazily connected MCP server and its transport.
type Session struct {
	Spec    Spec
	options Options

	loadMu  sync.Mutex
	stderr  mcpDiagnostics
	redact  func(string) string
	lastErr error

	once    sync.Once
	onceErr error

	mu     sync.Mutex
	cancel context.CancelFunc
	client *mcpClient
	listed bool
	tools  []ToolSpec
}

// NewSession returns a session that connects on Connect or LoadTools.
func NewSession(s Spec, options Options) *Session {
	s.Args = nonNil(s.Args)
	s.SecretEnvs = nonNil(s.SecretEnvs)
	return &Session{Spec: s, options: options}
}

func (m *Session) Name() string { return "mcp:" + m.Spec.Name }

// LoadTools lists tools with a bounded RPC deadline and preserves failures.
func (m *Session) LoadTools(ctx context.Context) ([]ToolSpec, error) {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()
	if err := m.Connect(ctx); err != nil {
		return nil, err
	}
	if err := m.client.failure(); err != nil {
		return nil, m.diagnostic("connection", err)
	}
	m.mu.Lock()
	if m.listed {
		out := m.tools
		m.mu.Unlock()
		return out, nil
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, mcpRPCWait)
	defer cancel()
	specs, err := m.client.listTools(ctx)
	if err != nil {
		return nil, m.diagnostic("tools/list", err)
	}
	list := make([]ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) != "" {
			list = append(list, spec)
		}
	}
	if len(list) == 0 {
		return nil, m.diagnostic("tools/list", errors.New("server returned no tools"))
	}
	m.mu.Lock()
	m.tools, m.listed, m.lastErr = list, true, nil
	m.mu.Unlock()
	return list, nil
}

// CallTool returns the server's raw tools/call result without model projection.
func (m *Session) CallTool(ctx context.Context, name string, arguments any) (json.RawMessage, error) {
	if err := m.Connect(ctx); err != nil {
		return nil, err
	}
	return m.client.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
}

// Notifications exposes the bounded, dropping notification channel after Connect.
func (m *Session) Notifications() <-chan string { return m.client.notes }

// Close stops the server process. It is safe to call more than once.
func (m *Session) Close() error {
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

// Connect starts and initializes the transport at most once.
func (m *Session) Connect(ctx context.Context) error {
	m.once.Do(func() {
		m.onceErr = m.start(ctx)
	})
	return m.onceErr
}

func (m *Session) start(caller context.Context) error {
	m.redact = m.diagnosticRedactor()
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := m.open(ctx)
	if err != nil {
		cancel()
		return m.diagnostic("start", err)
	}
	client := newMCPClient(conn)
	m.mu.Lock()
	m.cancel = cancel
	m.client = client
	m.mu.Unlock()
	go client.readLoop()
	initCtx, initCancel := context.WithTimeout(caller, mcpRPCWait)
	defer initCancel()
	if err := client.initialize(initCtx); err != nil {
		_ = m.Close()
		return m.diagnostic("initialize", err)
	}
	return nil
}

func (m *Session) open(ctx context.Context) (io.ReadWriteCloser, error) {
	if m.Spec.Dial != nil {
		return m.Spec.Dial(ctx)
	}
	if strings.TrimSpace(m.Spec.URL) != "" {
		return openStreamable(ctx, m.Spec, m.options)
	}
	if err := validateCommand(m.Spec.Command); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, m.Spec.Command, m.Spec.Args...)
	cmd.Env = spawnEnv(m.Spec.SecretEnvs, m.Spec.secretLookup(), m.options.providerKey)
	cmd.Stderr = &m.stderr
	cmd.WaitDelay = time.Second
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
	reader := &mcpProcessReader{Reader: stdout, cmd: cmd}
	return &stdioConn{Reader: reader, w: stdin, c: stdout, wait: reader.wait}, nil
}
