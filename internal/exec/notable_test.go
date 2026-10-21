package exec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/notable"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type effectsModel struct {
	calls []provider.ToolCall
	sent  bool
}

func (p *effectsModel) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	if !p.sent {
		p.sent = true
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: p.calls}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: "处理好了。"}, nil
}

type removeEffectTool struct{}

func (removeEffectTool) Name() string                                  { return "delete_file" }
func (removeEffectTool) Description() string                           { return "delete a test file" }
func (removeEffectTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (removeEffectTool) Confirm(json.RawMessage) (bool, string, error) { return true, "delete", nil }
func (removeEffectTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct{ Path string }
	_ = json.Unmarshal(args, &in)
	return "deleted", os.Remove(in.Path)
}

func TestNotableExecutionMetaAndReportStayTogether(t *testing.T) {
	work := t.TempDir()
	outside := t.TempDir()
	installer := filepath.Join(outside, "npm")
	if err := os.WriteFile(installer, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	defer service.Close()
	args := func(v any) string {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	cases := []struct {
		name, tool, raw, kind string
		allow                 bool
	}{
		{"install", "shell", args(map[string]any{"command": installer + " i -g demo"}), "global_install", true},
		{"outside", "write_file", args(map[string]any{"path": filepath.Join(outside, "new.txt"), "content": "ok"}), "write_outside", true},
		{"send", "http_request", args(map[string]any{"url": service.URL, "method": "POST", "body": "hello"}), "external_message", true},
		{"delete", "delete_file", args(map[string]any{"path": filepath.Join(outside, "delete.txt")}), "delete", true},
		{"ordinary", "write_file", args(map[string]any{"path": "local.txt", "content": "ok"}), "", true},
		{"shell failed", "shell", args(map[string]any{"command": "rm /path-that-does-not-exist-m7"}), "", true},
		{"denied", "delete_file", args(map[string]any{"path": filepath.Join(outside, "delete.txt")}), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(outside, "delete.txt"), []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			loc := NewLocal(Limits{MaxSteps: 3, Timeout: 5 * time.Second})
			loc.Prov = &effectsModel{calls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: tc.tool, Arguments: tc.raw}}}}
			sh := tools.NewShell()
			sh.Dir = work
			loc.ToolSet = func(string, string) []tools.Tool {
				return []tools.Tool{sh, &tools.WriteFile{Root: work}, &tools.HTTPRequest{}, removeEffectTool{}}
			}
			loc.Approve = func(context.Context, string, string) (bool, error) { return tc.allow, nil }
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			dir := t.TempDir()
			if err := loc.Start(ctx, StartRequest{ID: "sa_0123456789abcdef", Task: "test", Mode: ModeBrief, Dir: dir, Workspace: work}); err != nil {
				t.Fatal(err)
			}
			var ev Event
			for ev.Kind != EvDone && ev.Kind != EvFailed {
				select {
				case ev = <-loc.Events():
				case <-ctx.Done():
					t.Fatal("no completion")
				}
			}
			if ev.Kind != EvDone {
				t.Fatalf("event %+v", ev)
			}

			meta, err := ReadMeta(dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind == "" {
				if len(meta.Notable) != 0 || strings.Contains(ev.Text, "值得留意") {
					t.Fatalf("unperformed/ordinary effect reported: %+v / %s", meta.Notable, ev.Text)
				}
				return
			}
			if len(meta.Notable) != 1 || meta.Notable[0].Kind != tc.kind || !strings.Contains(ev.Text, notable.Line(meta.Notable)) {
				t.Fatalf("meta/report disagree: %+v / %s", meta.Notable, ev.Text)
			}
			if len(meta.Notable[0].Objects) == 0 {
				t.Fatal("real execution lost its objects")
			}
			if strings.Contains(meta.Summary, "值得留意") {
				t.Fatal("task summary duplicates separate notable line")
			}
		})
	}
}

func TestRemotePreservesWorkspaceAndNotable(t *testing.T) {
	work := t.TempDir()
	outside := t.TempDir()
	raw, _ := json.Marshal(map[string]string{"path": filepath.Join(outside, "remote.txt"), "content": "ok"})
	loc := NewLocal(Limits{MaxSteps: 3})
	loc.Prov = &effectsModel{calls: []provider.ToolCall{{ID: "c", Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(raw)}}}}
	loc.ToolSet = func(string, string) []tools.Tool { return []tools.Tool{&tools.WriteFile{Root: work}} }
	server := httptest.NewServer((&Server{Local: loc, Token: "test-token"}).Handler())
	defer server.Close()
	remote := Dial(server.URL, "test-token")
	defer remote.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dir := t.TempDir()
	if err := remote.Start(ctx, StartRequest{ID: "sa_0000000000000009", Mode: ModeBrief, Task: "write", Dir: dir, Workspace: work}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case ev := <-remote.Events():
			if ev.Kind != EvDone {
				continue
			}
			m, err := ReadMeta(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Notable) != 1 || !strings.Contains(ev.Text, notable.Line(m.Notable)) {
				t.Fatalf("runner lost workspace/effects: %+v / %s", m.Notable, ev.Text)
			}
			return
		case <-ctx.Done():
			t.Fatal("remote did not finish")
		}
	}
}
