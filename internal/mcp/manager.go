package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
)

var (
	ErrClosed        = errors.New("mcp: owner closed")
	ErrConfigChanged = errors.New("mcp: configuration_changed")
	ErrPaused        = errors.New("mcp: calls paused")
)

// Binding supplies short configuration IO and the existing notification route.
// ReadConfig must return the effective global/local configuration for this Bot.
type Binding struct {
	Key        string
	ReadConfig func() ([]Spec, error)
	Notify     func(server, text string) error
}

// Manager owns the MCP sessions for one running host. Its map lock never covers
// configuration IO, notifications, or transport work.
type Manager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	options Options
	mu      sync.Mutex
	refs    map[string]*BotRef
	closed  bool
	once    sync.Once
	err     error
}

func NewManager(ctx context.Context, options Options) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{ctx: ctx, cancel: cancel, options: options, refs: map[string]*BotRef{}}
	go func() { <-ctx.Done(); _ = m.Close() }()
	return m
}

// Bind publishes the initial effective specs synchronously. Transport handshakes
// and notification pumps start asynchronously after that publication.
func (m *Manager) Bind(binding Binding) (*BotRef, error) {
	if binding.ReadConfig == nil {
		return nil, errors.New("mcp: missing configuration reader")
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if _, ok := m.refs[binding.Key]; ok {
		m.mu.Unlock()
		return nil, errors.New("mcp: binding already exists")
	}
	r := &BotRef{manager: m, key: binding.Key, binding: binding, slots: map[string]*sessionSlot{}, reconcile: make(chan struct{}, 1)}
	m.refs[binding.Key] = r
	m.mu.Unlock()
	if err := r.Reconcile(m.ctx); err != nil {
		_ = m.Unbind(r)
		return nil, err
	}
	return r, nil
}

func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	refs := make([]*BotRef, 0, len(m.refs))
	for _, ref := range m.refs {
		refs = append(refs, ref)
	}
	m.mu.Unlock()
	var errs []error
	for _, ref := range refs {
		if err := ref.Reconcile(ctx); err != nil && !errors.Is(err, ErrClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Rekey only changes the host index. The binding's reader and notification
// callbacks follow the host's current resource route and remain unchanged.
func (m *Manager) Rekey(ref *BotRef, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || ref == nil || ref.manager != m || m.refs[ref.key] != ref {
		return ErrClosed
	}
	ref.mu.Lock()
	defer ref.mu.Unlock()
	if ref.closed {
		return ErrClosed
	}
	if other := m.refs[key]; other != nil && other != ref {
		return errors.New("mcp: binding already exists")
	}
	delete(m.refs, ref.key)
	ref.key = key
	m.refs[key] = ref
	return nil
}

func (m *Manager) Unbind(ref *BotRef) error {
	if ref == nil || ref.manager != m {
		return ErrClosed
	}
	// Retain the index until close finishes, so a concurrent Bind cannot start
	// a second session for the same key while this one's transport is closing.
	err := ref.close()
	m.mu.Lock()
	if m.refs[ref.key] == ref {
		delete(m.refs, ref.key)
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) Close() error {
	m.once.Do(func() {
		m.cancel()
		m.mu.Lock()
		m.closed = true
		refs := make([]*BotRef, 0, len(m.refs))
		for _, ref := range m.refs {
			refs = append(refs, ref)
		}
		m.mu.Unlock()
		var errs []error
		for _, ref := range refs {
			errs = append(errs, ref.close())
		}
		m.err = errors.Join(errs...)
	})
	return m.err
}

// BotRef borrows the sessions owned by Manager. Specs and Status read only
// already-published state. The reconcile gate orders config publication; it is
// separate from the short map lock and does not hold up calls or status reads.
type BotRef struct {
	manager   *Manager
	key       string // protected by manager.mu
	binding   Binding
	reconcile chan struct{}
	mu        sync.Mutex
	slots     map[string]*sessionSlot
	specs     []Spec
	closed    bool
	paused    bool
	once      sync.Once
	err       error
}

// Pause blocks new tools/call admission while keeping discovery and inbound
// notifications on the same live sessions.
func (r *BotRef) Pause() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.manager.ctx.Err() != nil {
		return ErrClosed
	}
	r.paused = true
	return nil
}

func (r *BotRef) Resume() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.manager.ctx.Err() != nil {
		return ErrClosed
	}
	r.paused = false
	return nil
}

func (r *BotRef) Specs() []Spec {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Spec, len(r.specs))
	for i, spec := range r.specs {
		out[i] = copySpec(spec)
	}
	return out
}

func (r *BotRef) Reconcile(ctx context.Context) error {
	select {
	case r.reconcile <- struct{}{}:
		defer func() { <-r.reconcile }()
	case <-ctx.Done():
		return ctx.Err()
	case <-r.manager.ctx.Done():
		return ErrClosed
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return ErrClosed
	}
	specs, configErr := r.binding.ReadConfig()
	if configErr != nil {
		// A malformed external file does not replace the last effective specs.
		// Their credentials still need refreshing: a config error must not keep
		// a deleted or rotated credential alive in an old transport.
		r.mu.Lock()
		specs = append([]Spec(nil), r.specs...)
		r.mu.Unlock()
	}
	type input struct {
		spec    Spec
		secrets map[string]string
		err     error
	}
	inputs := make([]input, 0, len(specs))
	for _, spec := range specs {
		spec = copySpec(spec)
		secrets, err := r.resolveSecrets(ctx, spec)
		inputs = append(inputs, input{spec: spec, secrets: secrets, err: err})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed || r.manager.ctx.Err() != nil {
		r.mu.Unlock()
		return ErrClosed
	}
	next := make(map[string]*sessionSlot, len(inputs))
	type replacement struct{ slot, previous *sessionSlot }
	var starts []replacement
	for _, in := range inputs {
		old := r.slots[in.spec.Name]
		if old != nil && sameSpec(old.spec, in.spec) && maps.Equal(old.secrets, in.secrets) && sameError(old.resolveErr, in.err) {
			next[in.spec.Name] = old
			continue
		}
		slot := r.newSlot(in.spec, in.secrets, in.err)
		next[in.spec.Name] = slot
		starts = append(starts, replacement{slot, old})
	}
	var removed []*sessionSlot
	for name, old := range r.slots {
		if next[name] == nil {
			removed = append(removed, old)
		}
	}
	r.slots = next
	r.specs = make([]Spec, len(inputs))
	for i, in := range inputs {
		r.specs[i] = in.spec
	}
	r.mu.Unlock()
	for _, start := range starts {
		go start.slot.run(start.previous)
	}
	for _, old := range removed {
		_ = old.stop()
	}
	return configErr
}

func (r *BotRef) resolveSecrets(ctx context.Context, spec Spec) (map[string]string, error) {
	names := make([]string, 0, len(spec.SecretEnvs))
	for _, name := range spec.SecretEnvs {
		if !r.manager.options.providerKey(name) {
			names = append(names, name)
		}
	}
	var resolved map[string]string
	if resolve := r.manager.options.ResolveSecrets; resolve != nil {
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

func (r *BotRef) LoadTools(ctx context.Context, server string) ([]ToolSpec, error) {
	slot, err := r.slot(server)
	if err != nil {
		return nil, err
	}
	if err := slot.wait(ctx); err != nil {
		return nil, err
	}
	if !r.current(server, slot) {
		return nil, ErrConfigChanged
	}
	return slot.session.LoadTools(ctx)
}

func (r *BotRef) CallTool(ctx context.Context, server, remote string, arguments any) (json.RawMessage, error) {
	slot, err := r.slot(server)
	if err != nil {
		return nil, err
	}
	return r.callTool(ctx, server, remote, arguments, slot)
}

// CallToolForSpec admits an adapter call only against the configuration whose
// schema and annotations the caller received. It never redirects to a new slot.
func (r *BotRef) CallToolForSpec(ctx context.Context, expected Spec, remote string, arguments any) (json.RawMessage, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	slot := r.slots[expected.Name]
	if slot == nil || !sameSpec(slot.spec, expected) {
		r.mu.Unlock()
		return nil, ErrConfigChanged
	}
	r.mu.Unlock()
	return r.callTool(ctx, expected.Name, remote, arguments, slot)
}

func (r *BotRef) callTool(ctx context.Context, server, remote string, arguments any, slot *sessionSlot) (json.RawMessage, error) {
	if err := slot.wait(ctx); err != nil {
		return nil, err
	}
	r.mu.Lock()
	var admission error
	switch {
	case r.closed || r.manager.ctx.Err() != nil:
		admission = ErrClosed
	case r.paused:
		admission = ErrPaused
	case r.slots[server] != slot:
		admission = ErrConfigChanged
	}
	r.mu.Unlock()
	if admission != nil {
		return nil, admission
	}
	raw, err := slot.session.CallTool(ctx, remote, arguments)
	if err != nil && !r.current(server, slot) {
		return nil, fmt.Errorf("%w: outcome_unknown", ErrConfigChanged)
	}
	return raw, err
}

func (r *BotRef) Status(server string) Status {
	slot, err := r.slot(server)
	if err != nil {
		return Status{State: "error", Error: err.Error()}
	}
	return slot.session.Status()
}

func (r *BotRef) slot(server string) (*sessionSlot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	slot := r.slots[server]
	if slot == nil {
		return nil, ErrMCPMissing
	}
	return slot, nil
}

func (r *BotRef) current(server string, slot *sessionSlot) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && r.slots[server] == slot
}

func (r *BotRef) close() error {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		slots := make([]*sessionSlot, 0, len(r.slots))
		for _, slot := range r.slots {
			slots = append(slots, slot)
		}
		r.mu.Unlock()
		var errs []error
		for _, slot := range slots {
			errs = append(errs, slot.stop())
		}
		r.err = errors.Join(errs...)
	})
	return r.err
}

// Each immutable slot has one startup/pump goroutine. A replacement waits only
// for the previous transport and pump to stop, never for calls to drain.
type sessionSlot struct {
	ref        *BotRef
	spec       Spec
	secrets    map[string]string
	resolveErr error
	ctx        context.Context
	cancel     context.CancelFunc
	session    *Session
	ready      chan struct{}
	done       chan struct{}
}

func (r *BotRef) newSlot(spec Spec, secrets map[string]string, resolveErr error) *sessionSlot {
	ctx, cancel := context.WithCancel(r.manager.ctx)
	options := r.manager.options
	options.lifetime = ctx
	options.secrets = secrets
	if options.secrets == nil {
		options.secrets = map[string]string{}
	}
	session := NewSession(spec, options)
	return &sessionSlot{ref: r, spec: spec, secrets: secrets, resolveErr: resolveErr, ctx: ctx, cancel: cancel, session: session, ready: make(chan struct{}), done: make(chan struct{})}
}

func (s *sessionSlot) run(previous *sessionSlot) {
	defer close(s.done)
	defer s.session.Close()
	if previous != nil {
		_ = previous.stop()
	}
	if s.resolveErr != nil {
		s.session.redact = s.session.diagnosticRedactor()
		_ = s.session.diagnostic("credentials", s.resolveErr)
		close(s.ready)
		return
	}
	if s.ctx.Err() != nil {
		close(s.ready)
		return
	}
	err := s.session.Connect(s.ctx)
	close(s.ready)
	if err != nil {
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case text, ok := <-s.session.Notifications():
			if !ok {
				return
			}
			if notify := s.ref.binding.Notify; notify != nil {
				if err := notify(s.spec.Name, text); err != nil {
					_ = s.session.diagnostic("notification", err)
					return
				}
			}
		}
	}
}

func (s *sessionSlot) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrConfigChanged
	case <-s.ready:
		if s.resolveErr != nil {
			return errors.New(s.session.Status().Error)
		}
		return nil
	}
}

func (s *sessionSlot) stop() error {
	s.cancel()
	err := s.session.Close()
	<-s.done
	return err
}

func copySpec(s Spec) Spec {
	s.Args = slices.Clone(s.Args)
	s.SecretEnvs = slices.Clone(s.SecretEnvs)
	return s
}

func sameSpec(a, b Spec) bool {
	return a.Name == b.Name && a.Command == b.Command && a.URL == b.URL && slices.Equal(a.Args, b.Args) && slices.Equal(a.SecretEnvs, b.SecretEnvs)
}

func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}
