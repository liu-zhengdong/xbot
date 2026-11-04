package deviceexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/desktop"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("XBOT_VAULT_SOCKET", "")
	m, err := New(t.TempDir(), "target", "Target computer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}
func testSource() Source {
	return Source{Account: "account", DeviceID: "origin", Bot: "alpha", ContextID: "main"}
}
func testCall(ref, kind, tool string, args any) Call {
	body, _ := json.Marshal(args)
	return Call{Ref: ref, Kind: kind, Tool: tool, Args: body, Description: "test " + ref}
}
func submit(t *testing.T, m *Manager, s Source, c Call) Task {
	t.Helper()
	task, err := m.Submit(context.Background(), s, c)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func waitDetail(t *testing.T, m *Manager, s Source, ref string) Detail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, err := m.Detail(s, ref, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if d.Task.Status != "running" {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return Detail{}
}
func requireShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
}

func TestExecutionUsesTargetWithoutBotAndDeduplicates(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	marker := filepath.Join(m.Root, "marker")
	c := testCall("stable", "exec", "sh", map[string]any{"command": fmt.Sprintf("printf x >> '%s'; printf accepted", marker)})
	first := submit(t, m, s, c)
	second := submit(t, m, s, c)
	foreign := s
	foreign.ContextID = "another-context"
	if _, err := m.Submit(context.Background(), foreign, c); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-context duplicate executed: %v", err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("duplicate jobs: %+v %+v", first, second)
	}
	d := waitDetail(t, m, s, c.Ref)
	if d.Task.Status != "completed" || !strings.Contains(d.Output, "accepted") {
		t.Fatalf("result: %+v", d)
	}
	if d.Task.DeviceID != "target" || d.Task.Bot != "alpha" {
		t.Fatalf("identity: %+v", d.Task)
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != "x" {
		t.Fatalf("executed twice: %q %v", body, err)
	}
	if _, err = os.Stat(filepath.Join(m.Root, "bots")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected target bot directory: %v", err)
	}
	conflict := c
	conflict.Args = json.RawMessage(`{"command":"printf changed"}`)
	if _, err = m.Submit(context.Background(), s, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("ref conflict: %v", err)
	}
	m.Close()
	reopened, err := New(m.Root, "target", "Target computer")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again := submit(t, reopened, s, c)
	if again.ID != first.ID {
		t.Fatalf("restart replay: %+v", again)
	}
	body, _ = os.ReadFile(marker)
	if string(body) != "x" {
		t.Fatalf("restart repeated execution: %q", body)
	}
}

func TestContextsKeepIndependentCwdAndJobs(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	a := testSource()
	b := a
	b.ContextID = "child"
	c := a
	c.Bot = "beta"
	submit(t, m, a, testCall("cd", "exec", "sh", map[string]any{"command": "mkdir -p nested; cd nested"}))
	if d := waitDetail(t, m, a, "cd"); d.Task.Status != "completed" {
		t.Fatalf("cd: %+v", d)
	}
	for _, s := range []Source{a, b, c} {
		ref := "pwd-" + s.Bot + "-" + s.ContextID
		submit(t, m, s, testCall(ref, "exec", "sh", map[string]any{"command": "pwd"}))
		d := waitDetail(t, m, s, ref)
		want := filepath.Join(m.Root, "deviceexec", "contexts", contextKey(s), "work")
		if s == a {
			want = filepath.Join(want, "nested")
		}
		physical, err := filepath.EvalSymlinks(want)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(d.Output, physical) {
			t.Fatalf("cwd for %+v: %q want %q", s, d.Output, physical)
		}
		if len(m.List(&s)) == 0 {
			t.Fatal("missing own jobs")
		}
	}
	if _, err := m.Detail(b, "cd", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign context found job: %v", err)
	}
	submit(t, m, a, testCall("write", "write", "write_file", map[string]any{"path": "note.txt", "content": "in cwd"}))
	write := waitDetail(t, m, a, "write")
	if len(write.Task.Artifacts) != 1 {
		t.Fatalf("file artifact: %+v", write)
	}
	submit(t, m, a, testCall("read", "read", "read_file", map[string]any{"path": "note.txt"}))
	if d := waitDetail(t, m, a, "read"); !strings.Contains(d.Output, "in cwd") {
		t.Fatalf("read cwd: %+v", d)
	}
}

func TestLiveOutputAndStopUseOriginalJobs(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	marker := filepath.Join(m.Root, "must-not-exist")
	call := testCall("long", "exec", "sh", map[string]any{"command": fmt.Sprintf("printf ready; sleep 30; printf bad > '%s'", marker), "timeout_seconds": 1})
	submit(t, m, s, call)
	deadline := time.Now().Add(3 * time.Second)
	var d Detail
	for time.Now().Before(deadline) {
		var err error
		d, err = m.Detail(s, call.Ref, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(d.Output, "ready") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if d.Task.Status != "running" || !strings.Contains(d.Output, "ready") {
		t.Fatalf("live output: %+v", d)
	}
	if _, err := m.Stop(s, call.Ref); err != nil {
		t.Fatal(err)
	}
	d = waitDetail(t, m, s, call.Ref)
	if d.Task.Status != "cancelled" {
		t.Fatalf("stop: %+v", d)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled process continued: %v", err)
	}
}

func TestOutputAndArtifactBudgets(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	path := filepath.Join(m.Root, "large.bin")
	body := []byte(strings.Repeat("0123456789", 70000))
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	call := testCall("large", "exec", "sh", map[string]any{"command": fmt.Sprintf("head -c 100000 '%s'", path)})
	call.Artifacts = []string{path}
	submit(t, m, s, call)
	waitDetail(t, m, s, call.Ref)
	for _, tc := range []struct{ request, want int }{{0, DefaultOutputLimit}, {MaxOutputLimit + 100, MaxOutputLimit}, {73, 73}} {
		d, err := m.Detail(s, call.Ref, 0, tc.request)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Output) != tc.want || d.NextOffset != int64(tc.want) {
			t.Fatalf("limit %d: bytes=%d next=%d", tc.request, len(d.Output), d.NextOffset)
		}
	}
	var got []byte
	offset := int64(0)
	for {
		chunk, err := m.Download(s, call.Ref, "a1", offset, MaxArtifactChunk*2)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk.Data) > MaxArtifactChunk {
			t.Fatal("oversized artifact block")
		}
		got = append(got, chunk.Data...)
		offset = chunk.NextOffset
		if offset == chunk.Total {
			break
		}
	}
	if string(got) != string(body) {
		t.Fatal("artifact chunks lost bytes")
	}
}

func TestReadonlyRejectsMutationsAndPermitsReads(t *testing.T) {
	m := testManager(t)
	s := testSource()
	s.Readonly = true
	for _, tc := range []struct{ kind, tool string }{{"exec", "sh"}, {"write", "write_file"}, {"write", "edit_file"}, {"desktop", "desktop_click"}, {"desktop", "desktop_type"}, {"browser", "browser_open"}, {"browser", "browser_click"}, {"browser", "browser_stop"}} {
		c := testCall(tc.tool, tc.kind, tc.tool, map[string]any{})
		if _, err := m.Submit(context.Background(), s, c); !errors.Is(err, ErrReadonly) {
			t.Fatalf("%s: %v", tc.tool, err)
		}
	}
	if len(m.List(nil)) != 0 {
		t.Fatal("rejected mutation created a job")
	}
	path := filepath.Join(m.Root, "readable")
	if err := os.WriteFile(path, []byte("readonly content"), 0600); err != nil {
		t.Fatal(err)
	}
	submit(t, m, s, testCall("read", "read", "read_file", map[string]any{"path": path}))
	if d := waitDetail(t, m, s, "read"); d.Task.Status != "completed" || !strings.Contains(d.Output, "readonly content") {
		t.Fatalf("read: %+v", d)
	}
	driver := &sequenceDriver{}
	m.Desktop = &desktop.Link{Root: m.Root, Driver: driver}
	submit(t, m, s, testCall("snapshot", "desktop", "desktop_snapshot", map[string]any{"app": "test"}))
	if d := waitDetail(t, m, s, "snapshot"); d.Task.Status != "completed" || d.Output != "snapshot" {
		t.Fatalf("snapshot: %+v", d)
	}
	submit(t, m, s, testCall("browser-read", "browser", "browser_snapshot", map[string]any{}))
	if d := waitDetail(t, m, s, "browser-read"); d.Task.ID == "" {
		t.Fatal("browser snapshot was not admitted")
	}
}

type sequenceDriver struct {
	mu      sync.Mutex
	actions []string
	entered chan struct{}
	release chan struct{}
}

func (d *sequenceDriver) Execute(ctx context.Context, a desktop.Action) (string, error) {
	text := a.Text
	if text == "" {
		text = a.Name
	}
	d.mu.Lock()
	d.actions = append(d.actions, text)
	d.mu.Unlock()
	if text == "a1" && d.entered != nil {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return text, nil
}
func (*sequenceDriver) Screenshot(context.Context, string) error   { return nil }
func (*sequenceDriver) Click(context.Context, int, int) error      { return nil }
func (*sequenceDriver) Type(context.Context, string) error         { return nil }
func (*sequenceDriver) Scroll(context.Context, int) error          { return nil }
func (*sequenceDriver) Key(context.Context, string) error          { return nil }
func (*sequenceDriver) Idle(context.Context) (time.Duration, bool) { return 0, false }

func TestDesktopSequencesHoldManagerLockAcrossContexts(t *testing.T) {
	m := testManager(t)
	a := testSource()
	b := a
	b.Bot = "beta"
	driver := &sequenceDriver{entered: make(chan struct{}), release: make(chan struct{})}
	m.Desktop = &desktop.Link{Root: m.Root, Driver: driver}
	op := func(text string) Operation {
		body, _ := json.Marshal(map[string]any{"text": text})
		return Operation{Tool: "desktop_type", Args: body}
	}
	submit(t, m, a, Call{Ref: "a", Kind: "desktop", Steps: []Operation{op("a1"), op("a2")}})
	select {
	case <-driver.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first sequence did not start")
	}
	submit(t, m, b, Call{Ref: "b", Kind: "desktop", Steps: []Operation{op("b1"), op("b2")}})
	// A shell remains independent while the physical desktop is occupied.
	if runtime.GOOS != "windows" {
		submit(t, m, b, testCall("parallel", "exec", "sh", map[string]any{"command": "printf shell"}))
		if d := waitDetail(t, m, b, "parallel"); d.Task.Status != "completed" {
			t.Fatalf("parallel shell: %+v", d)
		}
	}
	close(driver.release)
	waitDetail(t, m, a, "a")
	waitDetail(t, m, b, "b")
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if strings.Join(driver.actions, ",") != "a1,a2,b1,b2" {
		t.Fatalf("interleaved desktop actions: %v", driver.actions)
	}
}

func TestOwnerManagesSameAccountAndCannotSubmit(t *testing.T) {
	m := testManager(t)
	a := testSource()
	b := a
	b.Bot = "beta"
	submit(t, m, a, testCall("a", "read", "read_file", map[string]any{"path": "missing"}))
	submit(t, m, b, testCall("b", "read", "read_file", map[string]any{"path": "missing"}))
	waitDetail(t, m, a, "a")
	waitDetail(t, m, b, "b")
	owner := Source{Account: a.Account, Owner: true}
	if len(m.List(&owner)) != 2 {
		t.Fatal("owner did not see both contexts")
	}
	if _, err := m.Detail(owner, "b", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stop(owner, "b"); err != nil {
		t.Fatal(err)
	}
	owner.Account = "other"
	if len(m.List(&owner)) != 0 {
		t.Fatal("owner saw another account")
	}
	if _, err := m.Detail(owner, "b", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner detail: %v", err)
	}
	owner.Account = a.Account
	if _, err := m.Submit(context.Background(), owner, testCall("owner", "exec", "sh", map[string]any{"command": "true"})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner submitted: %v", err)
	}
}

func TestFailedExitHasFailedTaskAndKeepsOutput(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	submit(t, m, s, testCall("fail", "exec", "sh", map[string]any{"command": "printf diagnostic; exit 7"}))
	d := waitDetail(t, m, s, "fail")
	if d.Task.Status != "failed" || !strings.Contains(d.Output, "diagnostic") || !strings.Contains(d.Task.Error, "exit 7") {
		t.Fatalf("failed exit: %+v", d)
	}
}
