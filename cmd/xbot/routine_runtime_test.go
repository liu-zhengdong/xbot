package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
	"xbot/internal/tools"
)

func TestRoutineRuntimeCLIEarlyDispatch(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	output := captureStdout(t, func() error {
		return run([]string{"--root", absent, "routine", "create", "--args-file", filepath.Join(absent, "missing"), "--help"})
	})
	if !strings.Contains(output, "--self") || !strings.Contains(output, "offline") {
		t.Fatalf("incomplete help: %s", output)
	}
	if err := run([]string{"--root", absent, "routine", "list", "--self", "--json"}); err == nil {
		t.Fatal("runtime command without runtime identity succeeded")
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help or failed runtime command initialized local root: %v", err)
	}
	t.Setenv("XBOT_ROOT", t.TempDir())
	if err := run([]string{"--root=", "routine", "list", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root fell back to environment: %v", err)
	}
	for _, args := range [][]string{{"migrate", "--dry-run"}, {"list", "--bot", "ada"}, {"update", "r1", "--bot", "ada", "--skills", "ship"}, {"list", "--bot", "--self"}} {
		if routineUsesRuntime(args) {
			t.Errorf("offline syntax changed by runtime environment: %q", args)
		}
	}
	for _, args := range [][]string{{"list", "--self"}, {"list", "--self=true"}, {"update", "r1", "--self", "--args", "{}"}, {"create", "--args", "{}"}, {"delete", "r1"}, {"--help"}} {
		if !routineUsesRuntime(args) {
			t.Errorf("runtime syntax missed early dispatch: %q", args)
		}
	}
}

func TestRoutineRuntimeCLIRequestsAndPartial(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "actual-source-call")
	calls := make(chan string, 4)
	empty := []cliops.RoutineView{}
	source, err := cliops.Start(root, cliops.Handlers{Routines: &cliops.RoutineHandlers{
		Create: func(ctx context.Context, in cliops.RoutineCreate) (cliops.RoutineWriteReply, error) {
			calls <- "create"
			if in.ID != "r1" || in.Prompt != " raw " || in.Precheck != nil || cliops.RoutineCallID(ctx) != "actual-source-call" {
				t.Errorf("create input=%+v call=%q", in, cliops.RoutineCallID(ctx))
			}
			return cliops.RoutineWriteReply{ID: in.ID, Applied: true, Stage: "complete"}, nil
		},
		List: func(ctx context.Context) (cliops.RoutineListReply, error) {
			calls <- "list"
			if cliops.RoutineCallID(ctx) != "" {
				t.Errorf("list sent inherited call header=%q", cliops.RoutineCallID(ctx))
			}
			return cliops.RoutineListReply{Routines: &empty}, nil
		},
		Update: func(ctx context.Context, id string, in cliops.RoutinePatch) (cliops.RoutineWriteReply, error) {
			calls <- "update"
			if id != "r1" || in.Enabled == nil || *in.Enabled || cliops.RoutineCallID(ctx) != "actual-source-call" {
				t.Errorf("update id=%q input=%+v call=%q", id, in, cliops.RoutineCallID(ctx))
			}
			return cliops.RoutineWriteReply{ID: id, Applied: true, Stage: "zone_index"}, errors.New("zone index failed")
		},
		Delete: func(ctx context.Context, id string) (cliops.RoutineWriteReply, error) {
			calls <- "delete"
			if id != "r1" {
				t.Errorf("delete id=%q", id)
			}
			return cliops.RoutineWriteReply{ID: id, Applied: true, Stage: "complete"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	var out bytes.Buffer
	if err := runRoutineRuntime(context.Background(), root, true, []string{"create", "--self", "--args-file", "-", "--json"}, strings.NewReader(`{"id":"r1","prompt":" raw ","precheck":"discard","precheck":null}`), &out); err != nil {
		t.Fatal(err)
	}
	var created cliops.RoutineWriteReply
	if err := json.Unmarshal(out.Bytes(), &created); err != nil || !created.Applied || created.ID != "r1" {
		t.Fatalf("create stdout=%s error=%v", out.String(), err)
	}
	out.Reset()
	if err := runRoutineRuntime(context.Background(), "", false, []string{"list", "--self", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var listed cliops.RoutineListReply
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || listed.Routines == nil || *listed.Routines == nil {
		t.Fatalf("list stdout=%s error=%v", out.String(), err)
	}
	out.Reset()
	err = runRoutineRuntime(context.Background(), "", false, []string{"update", "r1", "--args", `{"enabled":false}`, "--self", "--json"}, nil, &out)
	var partial cliops.RoutineWriteReply
	if decodeErr := json.Unmarshal(out.Bytes(), &partial); decodeErr != nil || err == nil || !partial.Applied || partial.Stage != "zone_index" || partial.Error != "zone index failed" {
		t.Fatalf("partial stdout=%s error=%v decode=%v", out.String(), err, decodeErr)
	}
	out.Reset()
	if err := runRoutineRuntime(context.Background(), "", false, []string{"delete", "--self", "r1"}, nil, &out); err != nil || !strings.Contains(out.String(), "applied: true") {
		t.Fatalf("delete stdout=%s error=%v", out.String(), err)
	}
	if len(calls) != 4 {
		t.Fatalf("requests=%d want four, with no write retry", len(calls))
	}
	out.Reset()
	if err := runRoutineRuntime(context.Background(), t.TempDir(), true, []string{"list", "--self"}, nil, &out); err == nil || len(calls) != 4 || out.Len() != 0 {
		t.Fatalf("mismatched root reached runtime: requests=%d stdout=%s error=%v", len(calls), out.String(), err)
	}
}

func TestRoutineRuntimeCLIHumanListShellBudget(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	views := make([]cliops.RoutineView, 300)
	for i := range views {
		views[i] = cliops.RoutineView{ID: "once", Prompt: "p", At: "2026-11-02T00:00:00Z", Enabled: false}
	}
	reply := cliops.RoutineListReply{Routines: &views, Count: len(views)}
	compact, _ := json.Marshal(reply)
	if len(compact)+1 >= tools.DefaultShellOutputBytes {
		t.Fatalf("compact fixture must fit Shell output: %d", len(compact)+1)
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(compact) }))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	var out bytes.Buffer
	if err := runRoutineRuntime(context.Background(), "", false, []string{"list", "--self"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("oversized human output must fail before writing: stdout bytes=%d error=%v", out.Len(), err)
	}
}

func TestRoutineRuntimeCLIArtifactAndOutputBudget(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	reply := cliops.RoutineListReply{Count: 10, File: "/workspace/results/routines.json", Access: "local", Bytes: 100000}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(reply)
		_, _ = w.Write(raw)
	}))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	var out bytes.Buffer
	if err := runRoutineRuntime(context.Background(), "", false, []string{"list", "--self"}, nil, &out); err != nil || !strings.Contains(out.String(), reply.File) || !strings.Contains(out.String(), "local") {
		t.Fatalf("artifact stdout=%s error=%v", out.String(), err)
	}
	out.Reset()
	views := []cliops.RoutineView{{ID: "r1"}}
	reply = cliops.RoutineListReply{Routines: &views, Count: 1}
	base, _ := json.Marshal(reply)
	views[0].Prompt = strings.Repeat("x", cliops.RoutineMaxReply-len(base))
	if err := runRoutineRuntime(context.Background(), "", false, []string{"list", "--self", "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("newline overflow printed partial JSON: stdout bytes=%d error=%v", out.Len(), err)
	}
}
