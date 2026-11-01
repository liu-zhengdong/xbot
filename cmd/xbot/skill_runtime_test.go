package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestSkillRuntimeCLIEarlyDispatch(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	output := captureStdout(t, func() error {
		return run([]string{"--root", absent, "skill", "write", "--body-file", filepath.Join(absent, "missing"), "--help"})
	})
	if !strings.Contains(output, "--self") || !strings.Contains(output, "offline") {
		t.Fatalf("incomplete help: %s", output)
	}
	if err := run([]string{"--root", absent, "skill", "read", "ship", "--self"}); err == nil {
		t.Fatal("runtime read without runtime identity succeeded")
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help or failed runtime command initialized local root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	if err := run([]string{"--root=", "skill", "read", "ship", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root fell back to environment: %v", err)
	}
}

func TestSkillRuntimeCLIReadWriteAndPartial(t *testing.T) {
	root := t.TempDir()
	canonical, err := cliops.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-XBot-Root") != canonical {
			t.Errorf("root header=%q", r.Header.Get("X-XBot-Root"))
		}
		switch r.URL.Path {
		case "/v1/skill/read":
			if r.Method != http.MethodGet || r.URL.Query().Get("id") != "Legacy / Name" {
				t.Errorf("read method/query=%s %s", r.Method, r.URL.RawQuery)
			}
			empty := ""
			_ = json.NewEncoder(w).Encode(cliops.SkillReadReply{Slug: "ship", Name: "ship", Description: "release", Body: &empty})
		case "/v1/skill/write":
			var update cliops.SkillWrite
			if r.Method != http.MethodPost {
				t.Errorf("write method=%s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Error(err)
			}
			if update.Slug != "ship" || update.Body == nil || update.Scope != "global" || update.Title != "发布" {
				t.Errorf("update=%+v", update)
			}
			reply := cliops.SkillWriteReply{Slug: "ship", Written: true, Scope: "global", Target: "global", UpdatedBots: []string{"global"}}
			if update.Body != nil && *update.Body == "stdin body\n" {
				reply.Error = "file written but scope update incomplete"
				w.WriteHeader(http.StatusBadRequest)
			} else if update.Body != nil && *update.Body != "" {
				t.Errorf("unexpected body=%q", *update.Body)
			}
			_ = json.NewEncoder(w).Encode(reply)
		default:
			t.Errorf("unexpected path=%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.URL)
	var out bytes.Buffer
	if err := runSkillRuntime(context.Background(), root, true, []string{"read", "Legacy / Name", "--self", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var read cliops.SkillReadReply
	if err := json.Unmarshal(out.Bytes(), &read); err != nil || read.Body == nil || *read.Body != "" {
		t.Fatalf("empty body not preserved: %s %v", out.String(), err)
	}
	out.Reset()
	args := []string{"write", "ship", "--description", "release", "--title", "发布", "--scope", "global", "--self", "--json", "--body-file", "-"}
	err = runSkillRuntime(context.Background(), "", false, args, strings.NewReader("stdin body\n"), &out)
	var partial cliops.SkillWriteReply
	if decodeErr := json.Unmarshal(out.Bytes(), &partial); decodeErr != nil || err == nil || !partial.Written || len(partial.UpdatedBots) != 1 || partial.Error == "" {
		t.Fatalf("partial output=%s error=%v decode=%v", out.String(), err, decodeErr)
	}
	out.Reset()
	args[len(args)-2], args[len(args)-1] = "--body", ""
	if err := runSkillRuntime(context.Background(), "", false, args, nil, &out); err != nil {
		t.Fatalf("empty body rejected: %v", err)
	}
	if calls != 3 {
		t.Fatalf("requests=%d want 3", calls)
	}
	out.Reset()
	if err := runSkillRuntime(context.Background(), t.TempDir(), true, []string{"read", "ship", "--self"}, nil, &out); err == nil || calls != 3 {
		t.Fatalf("root mismatch reached runtime: requests=%d error=%v", calls, err)
	}
}

func TestSkillRuntimeCLIRequestBudgets(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, err := io.ReadAll(r.Body)
		if err != nil || len(raw) != cliops.SkillMaxRequest {
			t.Errorf("request bytes=%d error=%v", len(raw), err)
		}
		_ = json.NewEncoder(w).Encode(cliops.SkillWriteReply{Slug: "ship", Written: true, Scope: "all", UpdatedBots: []string{}})
	}))
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.URL)
	empty := ""
	base, err := json.Marshal(cliops.SkillWrite{Slug: "ship", Description: "d", Body: &empty})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("x", cliops.SkillMaxRequest-len(base))
	args := []string{"write", "ship", "--description", "d", "--self", "--json", "--body-file", "-"}
	var out bytes.Buffer
	if err := runSkillRuntime(context.Background(), "", false, args, strings.NewReader(body), &out); err != nil {
		t.Fatalf("exact final request budget rejected: %v", err)
	}
	for _, content := range []string{body + "x", strings.Repeat("<", cliops.SkillMaxRequest/2)} {
		out.Reset()
		if err := runSkillRuntime(context.Background(), "", false, args, strings.NewReader(content), &out); err == nil || out.Len() != 0 || calls != 1 {
			t.Fatalf("oversize request sent or printed: error=%v requests=%d stdout=%d", err, calls, out.Len())
		}
	}
	oversized := strings.NewReader(strings.Repeat("x", cliops.SkillMaxRequest+100))
	if err := runSkillRuntime(context.Background(), "", false, args, oversized, &out); err == nil || oversized.Len() != 99 || calls != 1 {
		t.Fatalf("max+1 read not enforced: remaining=%d error=%v requests=%d", oversized.Len(), err, calls)
	}
}

func TestSkillRuntimeCLIArtifactAndOutputBudget(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	reply := cliops.SkillReadReply{Slug: "ship", Name: "ship", Description: "release", Preview: "preview", File: "/workspace/results/skill.md", Access: "local", Bytes: 100000}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := json.Marshal(reply)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	t.Setenv("XBOT_ENDPOINT", server.URL)
	var out bytes.Buffer
	if err := runSkillRuntime(context.Background(), "", false, []string{"read", "ship", "--self"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), reply.Preview) || !strings.Contains(out.String(), reply.File) || !strings.Contains(out.String(), "local") {
		t.Fatalf("artifact location missing: %s", out.String())
	}
	out.Reset()
	body := ""
	reply = cliops.SkillReadReply{Slug: "ship", Name: "ship", Description: "d", Body: &body}
	base, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	body = strings.Repeat("x", cliops.SkillMaxReply-len(base))
	if err := runSkillRuntime(context.Background(), "", false, []string{"read", "ship", "--self", "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("newline exceeded output budget: error=%v output=%d", err, out.Len())
	}
}
