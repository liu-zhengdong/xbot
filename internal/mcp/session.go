package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Options supplies credential inputs and diagnostic redaction without tying the
// protocol core to the application's credential store.
type Options struct {
	IsProviderKey  func(string) bool
	ResolveSecrets func(context.Context, []string) (map[string]string, error)
	Redact         func(string) string

	lifetime context.Context
	secrets  map[string]string // non-nil means a frozen, authoritative snapshot
}

func (o Options) secretLookup(spec Spec) func(string) (string, bool) {
	if o.secrets == nil {
		return spec.secretLookup()
	}
	return func(name string) (string, bool) {
		for _, declared := range spec.SecretEnvs {
			if name == declared {
				value, ok := o.secrets[name]
				return value, ok
			}
		}
		return os.LookupEnv(name)
	}
}

func (o Options) resolveSecrets(ctx context.Context, spec Spec) (map[string]string, error) {
	names := make([]string, 0, len(spec.SecretEnvs))
	for _, name := range spec.SecretEnvs {
		if !o.providerKey(name) {
			names = append(names, name)
		}
	}
	var resolved map[string]string
	if resolve := o.ResolveSecrets; resolve != nil {
		var err error
		resolved, err = resolve(ctx, names)
		if err != nil {
			return nil, err
		}
	} else {
		resolved = map[string]string{}
		lookup := spec.secretLookup()
		for _, name := range names {
			if value, ok := lookup(name); ok {
				resolved[name] = value
			}
		}
	}
	// Copy only declared inputs. The resolver may return a reused or broader map;
	// neither its later mutation nor unrelated credentials affect this session.
	snapshot := make(map[string]string, len(names))
	var missing []error
	for _, name := range names {
		value, ok := resolved[name]
		if ok {
			snapshot[name] = value
		}
		if !ok || value == "" {
			missing = append(missing, fmt.Errorf("mcp: missing credential %q", name))
		}
	}
	return snapshot, errors.Join(missing...)
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

	mu       sync.Mutex
	ctx      context.Context
	closed   bool
	readDone chan struct{}
	cancel   context.CancelFunc
	client   *mcpClient
	listed   bool
	tools    []ToolSpec
}

// Open creates an independent session with one authoritative credential
// snapshot and a lifetime bounded by ctx. The caller must close it.
func Open(ctx context.Context, spec Spec, options Options) (*Session, error) {
	secrets, err := options.resolveSecrets(ctx, spec)
	if err != nil {
		return nil, err
	}
	options.secrets = secrets
	options.lifetime = ctx
	session := NewSession(spec, options)
	if err := session.Connect(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

// NewSession returns a session that connects on Connect or LoadTools.
func NewSession(s Spec, options Options) *Session {
	s.Args = nonNil(s.Args)
	s.SecretEnvs = nonNil(s.SecretEnvs)
	parent := options.lifetime
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Session{Spec: s, options: options, ctx: ctx, cancel: cancel}
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
	done := m.readDone
	m.closed = true
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c != nil {
		err := c.close()
		if done != nil {
			<-done
		}
		c.controls.Wait()
		return err
	}
	return nil
}

// abort closes a damaged transport without joining the read loop: it may be
// called from that same loop while replying to a server request. The owner
// still calls Close to join the reader and control notifications.
func (m *Session) abort(cause error) {
	m.mu.Lock()
	m.closed = true
	c := m.client
	m.mu.Unlock()
	m.cancel()
	if c != nil {
		c.fail(cause)
		_ = c.close()
	}
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
	ctx := m.ctx
	if err := ctx.Err(); err != nil {
		return m.diagnostic("start", err)
	}
	conn, err := m.open(ctx)
	if err != nil {
		return m.diagnostic("start", err)
	}
	client := newMCPClient(conn, m.ctx, m.abort)
	if stdio, ok := conn.(*stdioConn); ok {
		stdio.abort = client.abortTransport
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = conn.Close()
		return m.diagnostic("start", context.Canceled)
	}
	m.client = client
	done := make(chan struct{})
	m.readDone = done
	m.mu.Unlock()
	go func() { defer close(done); client.readLoop() }()
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
	cmd.Env = spawnEnv(m.Spec.SecretEnvs, m.options.secretLookup(m.Spec), m.options.providerKey)
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
