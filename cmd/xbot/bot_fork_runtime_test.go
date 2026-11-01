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
	"reflect"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestBotForkRuntimeEarlyHelpAndRoot(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, args := range [][]string{{"--help"}, {"child", "--role-file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent, "bot-fork"}, args...)) })
		if !strings.Contains(output, "bot-fork NAME --self") || !strings.Contains(output, "Help works offline") {
			t.Fatalf("help=%q", output)
		}
	}
	for _, args := range [][]string{{"child", "--self", "--role=x"}, {"child", "--self", "--role-file=-"}, {"child", "--role=x"}, {"source", "child", "--self", "--role=x"}} {
		if err := run(append([]string{"--root", absent, "bot-fork"}, args...)); err == nil {
			t.Fatalf("runtime succeeded without source: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized root: %v", err)
	}
	t.Setenv("XBOT_ROOT", t.TempDir())
	if err := run([]string{"--root=", "bot-fork", "child", "--self", "--role=x"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root error=%v", err)
	}
	var out bytes.Buffer
	if err := runBotForkRuntime(context.Background(), t.TempDir(), true, []string{"child", "--self", "--role=x"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || out.Len() != 0 {
		t.Fatalf("root mismatch output=%q err=%v", out.String(), err)
	}
}

func TestBotForkRuntimeCLIFileStdinAndSourceIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "stale-diagnostic-must-not-select-source")
	t.Setenv("XBOT_CALL_ID", "original-prepared-call")
	role := strings.Repeat("界", 2730) + "ab" // exactly 8192 UTF-8 duties bytes, including after trim
	file := filepath.Join(t.TempDir(), "duties")
	if err := os.WriteFile(file, []byte(role), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	want := cliops.BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}, HandoffParts: 2, HandoffTotal: 2, GroupID: "g_actual", Stage: "complete"}
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{Fork: func(ctx context.Context, input cliops.BotFork) (cliops.BotForkReply, error) {
		calls++
		if input != (cliops.BotFork{Name: " child ", Role: role, Group: " project "}) || cliops.SourceCallID(ctx) != "original-prepared-call" {
			t.Errorf("name=%q role bytes=%d group=%q call ID=%q", input.Name, len(input.Role), input.Group, cliops.SourceCallID(ctx))
		}
		return want, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		args  []string
		stdin io.Reader
		json  bool
	}{
		{[]string{" child ", "--self", "--role-file", file, "--group", " project ", "--json"}, nil, true},
		{[]string{" child ", "--self", "--role-file=-", "--group", " project "}, strings.NewReader(role), false},
	} {
		var out bytes.Buffer
		if err := runBotForkRuntime(context.Background(), root, true, tc.args, tc.stdin, &out); err != nil {
			t.Fatal(err)
		}
		if tc.json {
			var got cliops.BotForkReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("reply=%s err=%v", out.String(), err)
			}
		} else if out.String() != cliops.FormatBotFork(want) {
			t.Fatalf("human output=%q", out.String())
		}
		if out.Len() > 1024 || strings.Contains(out.String(), "界") {
			t.Fatalf("role appeared in short receipt: bytes=%d", out.Len())
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2", calls)
	}
}

func TestBotForkRuntimeEarlyDispatchIgnoresOfflineEnvFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale")
	t.Setenv("XBOT_CALL_ID", "prepared")
	// The root's offline env loader must never replace the active source.
	if err := os.WriteFile(filepath.Join(root, "env"), []byte("XBOT_ENDPOINT=http://127.0.0.1:1\nXBOT_CALL_ID=offline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{Fork: func(ctx context.Context, input cliops.BotFork) (cliops.BotForkReply, error) {
		calls++
		if input != (cliops.BotFork{Name: "child", Role: "duties"}) || cliops.SourceCallID(ctx) != "prepared" {
			t.Errorf("input=%+v call ID=%q", input, cliops.SourceCallID(ctx))
		}
		return cliops.BotForkReply{Name: "child", Created: true, Stage: "complete"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	output := captureStdout(t, func() error {
		return run([]string{"--root", root, "bot-fork", "child", "--self", "--role=duties", "--json"})
	})
	if calls != 1 || !strings.Contains(output, `"created":true`) || os.Getenv("XBOT_ENDPOINT") != source.Endpoint || os.Getenv("XBOT_CALL_ID") != "prepared" {
		t.Fatalf("early dispatch calls=%d output=%q", calls, output)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("runtime initialized offline files: entries=%v err=%v", entries, err)
	}
}

func TestBotForkRuntimeLiteralFlagsAreData(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	var got cliops.BotFork
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{Fork: func(_ context.Context, input cliops.BotFork) (cliops.BotForkReply, error) {
		calls++
		got = input
		return cliops.BotForkReply{Stage: "validation"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		args []string
		want cliops.BotFork
	}{
		{[]string{"child", "--self", "--role", "--help"}, cliops.BotFork{Name: "child", Role: "--help"}},
		{[]string{"child", "--role", "--self", "--self"}, cliops.BotFork{Name: "child", Role: "--self"}},
		{[]string{"child", "--self", "--role", "--", "--group", "--help"}, cliops.BotFork{Name: "child", Role: "--", Group: "--help"}},
		{[]string{"--self", "--role=x", "--", "--help"}, cliops.BotFork{Name: "--help", Role: "x"}},
	} {
		var out bytes.Buffer
		if err := runBotForkRuntime(context.Background(), "", false, tc.args, nil, &out); err != nil || got != tc.want || strings.Contains(out.String(), "Usage:") {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d want=4", calls)
	}
}

func TestBotForkRuntimeRawAndEscapedInputBudgets(t *testing.T) {
	role := " \n" + strings.Repeat("\x00<&>\"", 2048) + "\n "
	file := filepath.Join(t.TempDir(), "duties")
	if err := os.WriteFile(file, []byte(role), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.BotForkCommand{{Name: "child", Role: role, Group: "project"}, {Name: "child", RoleFile: file, Group: "project"}, {Name: "child", RoleFile: "-", Group: "project"}} {
		input, err := readBotForkInput(command, strings.NewReader(role))
		if err != nil || input != (cliops.BotFork{Name: "child", Role: role, Group: "project"}) {
			t.Fatalf("input changed: role bytes=%d err=%v", len(input.Role), err)
		}
	}
	for _, tc := range []struct {
		command cliops.BotForkCommand
		stdin   string
	}{
		{cliops.BotForkCommand{Name: "child", Role: strings.Repeat("x", cliops.BotForkMaxRequest+1)}, ""},
		{cliops.BotForkCommand{Name: "child", RoleFile: "-"}, strings.Repeat("x", cliops.BotForkMaxRequest+1)},
		{cliops.BotForkCommand{Name: "child", Role: strings.Repeat("<", cliops.BotForkMaxRequest/6+1)}, ""},
		{cliops.BotForkCommand{Name: strings.Repeat("<", cliops.BotForkMaxRequest/6+1), Role: "x"}, ""},
		{cliops.BotForkCommand{Name: "child", Role: "x", Group: strings.Repeat("<", cliops.BotForkMaxRequest/6+1)}, ""},
	} {
		if _, err := readBotForkInput(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatalf("oversized input accepted: name bytes=%d role bytes=%d stdin bytes=%d", len(tc.command.Name), len(tc.command.Role), len(tc.stdin))
		}
	}
	base := `{"name":"child","role":""}`
	exact := strings.Repeat("x", cliops.BotForkMaxRequest-len(base))
	if _, err := readBotForkInput(cliops.BotForkCommand{Name: "child", Role: exact}, nil); err != nil {
		t.Fatalf("exact encoded limit: %v", err)
	}
	if _, err := readBotForkInput(cliops.BotForkCommand{Name: "child", Role: exact + "x"}, nil); err == nil {
		t.Fatal("encoded max+1 accepted")
	}
	reader := &botForkRuntimeCountingReader{reader: strings.NewReader(strings.Repeat("x", 2*cliops.BotForkMaxRequest))}
	if _, err := readBotForkInput(cliops.BotForkCommand{Name: "child", RoleFile: "-"}, reader); err == nil || reader.bytes != cliops.BotForkMaxRequest+1 {
		t.Fatalf("bounded read consumed=%d err=%v", reader.bytes, err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", cliops.BotForkMaxRequest+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBotForkInput(cliops.BotForkCommand{Name: "child", RoleFile: file}, nil); err == nil {
		t.Fatal("raw oversized file accepted")
	}
	if _, err := readBotForkInput(cliops.BotForkCommand{Name: "child", RoleFile: filepath.Join(t.TempDir(), "missing")}, nil); err == nil {
		t.Fatal("missing input file accepted")
	}
}

type botForkRuntimeCountingReader struct {
	reader io.Reader
	bytes  int
}

func (reader *botForkRuntimeCountingReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	reader.bytes += n
	return n, err
}

func TestBotForkRuntimePartialReceiptsPrintBeforeError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "original")
	var actual cliops.BotForkReply
	var sourceErr error
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{Fork: func(context.Context, cliops.BotFork) (cliops.BotForkReply, error) {
		calls++
		return actual, sourceErr
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		reply cliops.BotForkReply
		err   error
	}{
		{cliops.BotForkReply{Stage: "validation"}, errors.New("invalid name")},
		{cliops.BotForkReply{Name: "child", Created: true, CreateStage: "state", HandoffTotal: 2, Stage: "create"}, errors.New("state failed after config")},
		{cliops.BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &cliops.ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, Stage: "handoff_memory"}, errors.New("profile full")},
		{cliops.BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules", "handoff", "child_note"}, HandoffParts: 2, HandoffTotal: 2, UncertainMemory: &cliops.ForkMemoryAttempt{Bot: "current-source", Key: "fork-child"}, Stage: "source_note"}, errors.New("index failed; Markdown may exist")},
		{cliops.BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules", "handoff", "child_note", "source_note"}, HandoffParts: 2, HandoffTotal: 2, Stage: "group"}, errors.New("group failed")},
		{cliops.BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}, HandoffParts: 2, HandoffTotal: 2, GroupID: "g_actual", Stage: "complete"}, nil},
	} {
		actual, sourceErr = tc.reply, tc.err
		want := tc.reply
		if tc.err != nil {
			want.Error = tc.err.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"child", "--self", "--role=x"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runBotForkRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (tc.err != nil) || out.Len() == 0 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("out=%q err=%v wantErr=%v", out.String(), err, tc.err)
			}
			if asJSON {
				var got cliops.BotForkReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("got=%+v want=%+v decode=%v", got, want, err)
				}
			} else if out.String() != cliops.FormatBotFork(want) {
				t.Fatalf("human receipt changed: %q", out.String())
			}
		}
	}
	if calls != 12 {
		t.Fatalf("calls=%d want=12 without retries", calls)
	}
}

func TestBotForkRuntimeOversizedDisplayPreservesFacts(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	actual := cliops.BotForkReply{Name: strings.Repeat("\x00", 8<<10), Created: true, CreateStage: "persona", Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &cliops.ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, GroupID: "g_actual", Stage: "handoff_memory", Error: "late display failure"}
	body, err := json.Marshal(actual)
	if err != nil || len(body) <= 32<<10 || len(body) > cliops.BotForkMaxReply {
		t.Fatalf("fixture bytes=%d err=%v", len(body), err)
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
		args := []string{"child", "--self", "--role=x"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := runBotForkRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() == 0 || out.Len() > 32<<10 {
			t.Fatalf("bytes=%d err=%v", out.Len(), err)
		}
		if asJSON {
			var got cliops.BotForkReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := actual
			want.Name, want.Error = got.Name, got.Error
			if got.Name != "" || got.Error == "" || !reflect.DeepEqual(got, want) {
				t.Fatalf("facts lost: got=%+v want=%+v", got, want)
			}
		} else {
			for _, fact := range []string{"created: true\n", "create_stage: persona\n", "confirmed: owners, binding, rules\n", "handoff_parts: 2\n", "handoff_total: 9\n", "uncertain_memory: bot=\"child\" key=\"fork-handoff-3\"\n", "may already have written Markdown", "group_id: g_actual\n", "stage: handoff_memory\n"} {
				if !strings.Contains(out.String(), fact) {
					t.Errorf("human lost %q", fact)
				}
			}
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2 without retries", calls)
	}
}

func TestBotForkRuntimeEndpointFailureDoesNotFallThrough(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	for _, endpoint := range []string{"", "http://localhost:1", "http://127.0.0.1:1"} {
		t.Setenv("XBOT_ENDPOINT", endpoint)
		var out bytes.Buffer
		if err := runBotForkRuntime(context.Background(), "", false, []string{"child", "--self", "--role=x"}, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("endpoint=%q out=%q err=%v", endpoint, out.String(), err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("runtime failure initialized offline state: entries=%v err=%v", entries, err)
		}
	}
}

func TestBotForkRuntimeLegacyForkParserUnchanged(t *testing.T) {
	// The independent noun must not change the original valid two-name parser.
	// Parse only: the old offline writer must never be used as a runtime fallback.
	for _, tc := range []struct {
		args []string
		want forkOpts
	}{
		{[]string{"source", "child", "--role", "duties", "--group", "project"}, forkOpts{Src: "source", Name: "child", Role: "duties", Group: "project"}},
		{[]string{"--role=--help", "source", "child", "--group=--self"}, forkOpts{Src: "source", Name: "child", Role: "--help", Group: "--self"}},
	} {
		got, err := parseFork(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("old parser args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
}
