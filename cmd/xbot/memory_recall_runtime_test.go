package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestMemoryRecallRuntimeEarlyOfflineHelpAndNoFallthrough(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	t.Setenv("XBOT_CALL_ID", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	oldDefault := defaultCommand
	defaultCommand = "app"
	defer func() { defaultCommand = oldDefault }()
	for _, args := range [][]string{{"--help"}, {"--query-file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent, "memory-recall"}, args...)) })
		if !strings.Contains(output, "memory-recall --self") || !strings.Contains(output, "Help works offline") {
			t.Fatalf("help=%q", output)
		}
	}
	for _, args := range [][]string{{"--self"}, {"--query=x"}, {"--self", "--query", "--help"}, {"--self", "--query-file", "--"}, {"--self", "positional"}, {"help"}, {"-h"}} {
		if err := run(append([]string{"--root", absent, "memory-recall"}, args...)); err == nil {
			t.Fatalf("no source succeeded %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized absent root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	if err := run([]string{"--root=", "memory-recall", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root err=%v", err)
	}
	for _, endpoint := range []string{"", "http://localhost:1", "http://127.0.0.1:1"} {
		t.Setenv("XBOT_ENDPOINT", endpoint)
		t.Setenv("XBOT_CALL_ID", "actual-call")
		var out bytes.Buffer
		if err := runMemoryRecallRuntime(context.Background(), "", false, []string{"--self"}, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("endpoint=%q output=%q err=%v", endpoint, out.String(), err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failure initialized offline files entries=%v err=%v", entries, err)
	}
}

func TestMemoryRecallRuntimeCanonicalSourceFileStdinAndRequiredIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "actual-original-call")
	if err := os.WriteFile(filepath.Join(root, "env"), []byte("XBOT_ENDPOINT=http://127.0.0.1:1\nXBOT_ROOT=/absent\nXBOT_CALL_ID=wrong\n"), 0600); err != nil {
		t.Fatal(err)
	}
	query := " \n" + strings.Repeat("查询界", 3000) + "\n "
	file := filepath.Join(t.TempDir(), "query")
	if err := os.WriteFile(file, []byte(query), 0600); err != nil {
		t.Fatal(err)
	}
	var got cliops.MemoryRecall
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Recall: func(ctx context.Context, input cliops.MemoryRecall) (cliops.MemoryRecallReply, error) {
		calls++
		got = input
		if cliops.SourceCallID(ctx) != "actual-original-call" {
			t.Errorf("actual call ID=%q", cliops.SourceCallID(ctx))
		}
		return cliops.MemoryRecallReply{Text: "- person:notes.md:7: original text"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, inputArgs := range [][]string{{"--query", query}, {"--query-file", file}, {"--query-file=-"}} {
		args := append([]string{"--self", "--scope= BOT ", "--since= 2026-11-02 ", "--json"}, inputArgs...)
		var out bytes.Buffer
		if err := runMemoryRecallRuntime(context.Background(), root, true, args, strings.NewReader(query), &out); err != nil {
			t.Fatal(err)
		}
		if got != (cliops.MemoryRecall{Query: query, Scope: " BOT ", Since: " 2026-11-02 "}) {
			t.Fatalf("input normalized query bytes=%d scope=%q since=%q", len(got.Query), got.Scope, got.Since)
		}
		var reply cliops.MemoryRecallReply
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.Text != "- person:notes.md:7: original text" || !strings.HasSuffix(out.String(), "\n") {
			t.Fatalf("output=%q err=%v", out.String(), err)
		}
	}
	output := captureStdout(t, func() error {
		return run([]string{"--root", root, "memory-recall", "--self", "--query", "--help", "--scope", "--", "--since", "--help"})
	})
	if output != "- person:notes.md:7: original text\n" || got != (cliops.MemoryRecall{Query: "--help", Scope: "--", Since: "--help"}) || os.Getenv("XBOT_ENDPOINT") != source.Endpoint || os.Getenv("XBOT_CALL_ID") != "actual-original-call" {
		t.Fatalf("early dispatch output=%q input=%+v", output, got)
	}
	for _, args := range [][]string{{"--self"}, {"--self", "--query="}} {
		var out bytes.Buffer
		if err := runMemoryRecallRuntime(context.Background(), "", false, args, nil, &out); err != nil || got.Query != "" {
			t.Fatalf("missing/empty query changed got=%+v err=%v", got, err)
		}
	}
	var out bytes.Buffer
	if err := runMemoryRecallRuntime(context.Background(), t.TempDir(), true, []string{"--self"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || out.Len() != 0 {
		t.Fatalf("mismatch output=%q err=%v", out.String(), err)
	}
	t.Setenv("XBOT_CALL_ID", "")
	if err := runMemoryRecallRuntime(context.Background(), "", false, []string{"--self", "--query-file", filepath.Join(t.TempDir(), "missing")}, nil, &out); err == nil || !strings.Contains(err.Error(), "XBOT_CALL_ID") || out.Len() != 0 {
		t.Fatalf("missing call read input or called endpoint err=%v", err)
	}
	if calls != 6 {
		t.Fatalf("calls=%d want=6", calls)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("offline initialization entries=%v err=%v", entries, err)
	}
}

func TestMemoryRecallRuntimeRawAndEscapedQueryBudgets(t *testing.T) {
	file := filepath.Join(t.TempDir(), "query")
	for _, query := range []string{"", " \n\t ", "--help", "--", strings.Repeat("q", 7000)} {
		if err := os.WriteFile(file, []byte(query), 0600); err != nil {
			t.Fatal(err)
		}
		for _, command := range []cliops.MemoryRecallCommand{{Query: query}, {QueryFile: file}, {QueryFile: "-"}} {
			command.Scope, command.Since = " BOT ", " invalid "
			got, err := readMemoryRecallInput(command, strings.NewReader(query))
			if err != nil || got != (cliops.MemoryRecall{Query: query, Scope: command.Scope, Since: command.Since}) {
				t.Fatalf("query bytes=%d changed=%d err=%v", len(query), len(got.Query), err)
			}
		}
	}
	max := cliops.MemoryRecallMaxRequest
	reader := &memoryRecallRuntimeCountingReader{reader: strings.NewReader(strings.Repeat("x", 2*max))}
	if _, err := readMemoryRecallInput(cliops.MemoryRecallCommand{QueryFile: "-"}, reader); err == nil || reader.bytes != max+1 {
		t.Fatalf("bounded input consumed=%d err=%v", reader.bytes, err)
	}
	for _, tc := range []struct {
		command cliops.MemoryRecallCommand
		stdin   string
	}{
		{cliops.MemoryRecallCommand{Query: strings.Repeat("x", max+1)}, ""},
		{cliops.MemoryRecallCommand{QueryFile: "-"}, strings.Repeat("x", max+1)},
		{cliops.MemoryRecallCommand{Query: strings.Repeat("<", max/6+1)}, ""},
		{cliops.MemoryRecallCommand{Query: strings.Repeat("\x00", max/6+1)}, ""},
	} {
		if _, err := readMemoryRecallInput(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatal("oversized query accepted")
		}
	}
	base := `{"query":""}`
	exact := strings.Repeat("x", max-len(base))
	for _, query := range []string{exact, exact + "x"} {
		_, err := readMemoryRecallInput(cliops.MemoryRecallCommand{Query: query}, nil)
		if (err == nil) != (query == exact) {
			t.Fatalf("escaped request bytes=%d err=%v", len(query)+len(base), err)
		}
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", max+1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, filepath.Join(t.TempDir(), "missing")} {
		if _, err := readMemoryRecallInput(cliops.MemoryRecallCommand{QueryFile: path}, nil); err == nil {
			t.Fatal("bad file accepted")
		}
	}
}

type memoryRecallRuntimeCountingReader struct {
	reader io.Reader
	bytes  int
}

func (r *memoryRecallRuntimeCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}

func TestMemoryRecallRuntimePartialOutputBeforeFailureAndArtifactMetadata(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	t.Setenv("XBOT_CALL_ID", "actual-call")
	var actual cliops.MemoryRecallReply
	var sourceErr error
	calls := 0
	source, err := cliops.Start(os.Getenv("XBOT_ROOT"), cliops.Handlers{Recall: func(context.Context, cliops.MemoryRecall) (cliops.MemoryRecallReply, error) {
		calls++
		return actual, sourceErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	// The artifact deliberately does not exist. The CLI prints the supplied facts
	// without expanding the file or replacing the complete semantic text.
	for _, tc := range []struct {
		reply   cliops.MemoryRecallReply
		problem error
	}{
		{cliops.MemoryRecallReply{Text: strings.Repeat("- bot:notes.md:1: "+strings.Repeat("界", 200)+"…\n", 20)}, nil},
		{cliops.MemoryRecallReply{Text: "no matches"}, nil},
		{cliops.MemoryRecallReply{File: "/current/results/nonexistent.json", Access: "local", Bytes: 36712}, nil},
		{cliops.MemoryRecallReply{File: "/current/results/nonexistent.json", Access: "local", Bytes: 36712}, errors.New("late metadata failure")},
		{cliops.MemoryRecallReply{Text: "actual partial text"}, errors.New("source read failed")},
		{cliops.MemoryRecallReply{Error: "source context rejected"}, nil},
	} {
		actual, sourceErr = tc.reply, tc.problem
		want := actual
		if sourceErr != nil {
			want.Error = sourceErr.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"--self"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runMemoryRecallRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (want.Error != "") || out.Len() == 0 || out.Len() > 32<<10 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("output bytes=%d err=%v", out.Len(), err)
			}
			if asJSON {
				var got cliops.MemoryRecallReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || got != want {
					t.Fatalf("facts changed got=%+v want=%+v err=%v", got, want, err)
				}
			} else if out.String() != cliops.FormatMemoryRecall(want) {
				t.Fatalf("human changed bytes=%d want=%d", out.Len(), len(cliops.FormatMemoryRecall(want)))
			}
		}
	}
	if calls != 12 {
		t.Fatalf("calls=%d want=12 without retries", calls)
	}
}

func TestMemoryRecallRuntimeUnsafeErrorAndBothDisplayBudgets(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	t.Setenv("XBOT_CALL_ID", "actual-call")
	actual := cliops.MemoryRecallReply{File: "/current/results/recall.json", Access: "local", Bytes: 36712, Error: strings.Repeat("\x00", 8<<10)}
	body, err := json.Marshal(actual)
	if err != nil || len(body) <= 32<<10 || len(body) > cliops.MemoryRecallMaxReply {
		t.Fatal("invalid error fixture")
	}
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	for _, asJSON := range []bool{false, true} {
		args := []string{"--self"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := runMemoryRecallRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() == 0 || out.Len() > 32<<10 {
			t.Fatalf("error output bytes=%d err=%v", out.Len(), err)
		}
		if asJSON {
			var got cliops.MemoryRecallReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := actual
			want.Error = got.Error
			if got != want || got.Error == "" {
				t.Fatalf("metadata lost %+v", got)
			}
		} else if !strings.Contains(out.String(), actual.File+" (access: local, bytes: 36712)\n") {
			t.Fatalf("human metadata lost=%q", out.String())
		}
	}
	// A malformed source cannot make the client silently clip semantic text or
	// invent an artifact. Both CLI formats reject the same oversize facts.
	body, _ = json.Marshal(cliops.MemoryRecallReply{Text: strings.Repeat("\x00", 6<<10)})
	for _, args := range [][]string{{"--self"}, {"--self", "--json"}} {
		var out bytes.Buffer
		if err := runMemoryRecallRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("unbounded facts printed bytes=%d err=%v", out.Len(), err)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d want=4", calls)
	}
}
