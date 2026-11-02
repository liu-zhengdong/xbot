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
)

func TestCompactRuntimeOfflineAndCanonicalBoundSource(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	t.Setenv("XBOT_CALL_ID", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	oldDefault := defaultCommand
	defaultCommand = "app"
	defer func() { defaultCommand = oldDefault }()
	for _, args := range [][]string{{"--help"}, {"--self", "--help"}, {"--focus-file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error {
			return run(append([]string{"--root", absent, "compact"}, args...))
		})
		if output != compactUsage {
			t.Fatalf("offline help=%q", output)
		}
	}
	for _, args := range [][]string{{"--self", "--from=1", "--to=12"}, {"--from=1", "--to=12"}, {"--self", "--from=1", "--to=12", "--focus", "--help"}, {"--self", "--from=1", "--to=12", "--focus-file", "--"}, {"help"}, {"-h"}, {"--self", "positional"}} {
		if err := run(append([]string{"--root", absent, "compact"}, args...)); err == nil {
			t.Fatalf("missing source accepted args=%q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("offline path initialized: %v", err)
	}
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "old-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "actual-original-call")
	// An offline env file must not replace the current source environment.
	if err := os.WriteFile(filepath.Join(root, "env"), []byte("XBOT_ENDPOINT=http://127.0.0.1:1\nXBOT_CALL_ID=wrong\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var got cliops.Compact
	source, err := cliops.Start(root, cliops.Handlers{Compact: func(ctx context.Context, input cliops.Compact) (cliops.CompactReply, error) {
		calls++
		got = input
		if cliops.SourceCallID(ctx) != "actual-original-call" {
			t.Errorf("source call ID=%q", cliops.SourceCallID(ctx))
		}
		return cliops.CompactReply{Text: "compacted 1-12"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, explicitRoot := range []string{root, alias} {
		output := captureStdout(t, func() error {
			return run([]string{"--root", explicitRoot, "compact", "--self", "--from=0", "--to=-7", "--focus", "--help"})
		})
		if output != "compacted 1-12\n" || got != (cliops.Compact{From: 0, To: -7, Focus: "--help"}) {
			t.Fatalf("early runtime dispatch output=%q input=%+v", output, got)
		}
	}
	args := []string{"--self", "--from=1", "--to=12"}
	for _, given := range []string{"", t.TempDir()} {
		var out bytes.Buffer
		err := runCompactRuntime(context.Background(), given, true, args, nil, &out)
		if err == nil || out.Len() != 0 || (!strings.Contains(err.Error(), "explicit --root") && !strings.Contains(err.Error(), "mismatch")) {
			t.Fatalf("explicit root=%q output=%q err=%v", given, out.String(), err)
		}
	}
	for _, endpoint := range []string{"", "http://localhost:1", "https://127.0.0.1:1", "http://127.0.0.1:1/path", "http://127.0.0.1:1"} {
		t.Setenv("XBOT_ENDPOINT", endpoint)
		var out bytes.Buffer
		if err := runCompactRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("endpoint=%q output=%q err=%v", endpoint, out.String(), err)
		}
	}
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	t.Setenv("XBOT_CALL_ID", "")
	for _, focusArgs := range [][]string{{"--focus-file", filepath.Join(t.TempDir(), "missing")}, {"--focus-file=-"}} {
		reader := strings.NewReader("must remain unread")
		var out bytes.Buffer
		err := runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), focusArgs...), reader, &out)
		if err == nil || !strings.Contains(err.Error(), "XBOT_CALL_ID") || reader.Len() != len("must remain unread") || out.Len() != 0 {
			t.Fatalf("missing call ID consumed input or contacted runtime reader=%d err=%v", reader.Len(), err)
		}
	}
	if calls != 2 || os.Getenv("XBOT_ENDPOINT") != source.Endpoint {
		t.Fatalf("unexpected source calls=%d", calls)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("runtime path initialized offline state entries=%v err=%v", entries, err)
	}
}

func TestCompactRuntimeFocusFileStdinAndEscapedBudgets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "actual-original-call")
	calls := 0
	var got cliops.Compact
	source, err := cliops.Start(root, cliops.Handlers{Compact: func(ctx context.Context, input cliops.Compact) (cliops.CompactReply, error) {
		calls++
		got = input
		if cliops.SourceCallID(ctx) != "actual-original-call" {
			t.Errorf("source call ID=%q", cliops.SourceCallID(ctx))
		}
		return cliops.CompactReply{Text: "compacted 1-12"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	file := filepath.Join(t.TempDir(), "focus")
	args := []string{"--self", "--from=0", "--to=-1", "--json"}
	for _, focus := range []string{"", " \n\t ", "--help", "--", " \n" + strings.Repeat("查询界", 3000) + "\t "} {
		if err := os.WriteFile(file, []byte(focus), 0600); err != nil {
			t.Fatal(err)
		}
		for _, focusArgs := range [][]string{{"--focus", focus}, {"--focus-file", file}, {"--focus-file=-"}} {
			var out bytes.Buffer
			err := runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), focusArgs...), strings.NewReader(focus), &out)
			if err != nil || got != (cliops.Compact{From: 0, To: -1, Focus: focus}) || out.String() != "{\"text\":\"compacted 1-12\"}\n" {
				t.Fatalf("raw focus changed input bytes=%d got bytes=%d output=%q err=%v", len(focus), len(got.Focus), out.String(), err)
			}
		}
	}
	before := calls
	max := cliops.CompactMaxRequest
	reader := strings.NewReader(strings.Repeat("x", 2*max))
	var out bytes.Buffer
	err = runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), "--focus-file=-"), reader, &out)
	if err == nil || 2*max-reader.Len() != max+1 || calls != before || out.Len() != 0 {
		t.Fatalf("stdin max+1 consumed=%d calls=%d err=%v", 2*max-reader.Len(), calls, err)
	}
	for _, focus := range []string{strings.Repeat(" ", max+1), strings.Repeat("<", max/6+1), strings.Repeat("\x00", max/6+1)} {
		if err := os.WriteFile(file, []byte(focus), 0600); err != nil {
			t.Fatal(err)
		}
		for _, focusArgs := range [][]string{{"--focus", focus}, {"--focus-file", file}, {"--focus-file=-"}} {
			out.Reset()
			if err := runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), focusArgs...), strings.NewReader(focus), &out); err == nil || calls != before || out.Len() != 0 {
				t.Fatalf("raw/escaped focus accepted bytes=%d calls=%d err=%v", len(focus), calls, err)
			}
		}
	}
	base := `{"from":0,"to":-1,"focus":""}`
	exact := strings.Repeat("x", max-len(base))
	for _, focus := range []string{exact, exact + "x"} {
		out.Reset()
		err := runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), "--focus", focus), nil, &out)
		if focus == exact {
			if err != nil || calls != before+1 || got.Focus != focus || out.Len() == 0 {
				t.Fatalf("exact typed JSON budget failed calls=%d err=%v", calls, err)
			}
		} else if err == nil || calls != before+1 || out.Len() != 0 {
			t.Fatalf("typed JSON max+1 accepted calls=%d err=%v", calls, err)
		}
	}
	out.Reset()
	if err := runCompactRuntime(context.Background(), "", false, append(append([]string{}, args...), "--focus-file", filepath.Join(t.TempDir(), "missing")), nil, &out); err == nil || calls != before+1 || out.Len() != 0 {
		t.Fatalf("missing focus file accepted calls=%d err=%v", calls, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("CLI created offline state entries=%v err=%v", entries, err)
	}
}

func TestCompactRuntimePrintsErrorFactsBeforeFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "actual-original-call")
	calls := 0
	var actual cliops.CompactReply
	var sourceErr error
	source, err := cliops.Start(root, cliops.Handlers{Compact: func(ctx context.Context, input cliops.Compact) (cliops.CompactReply, error) {
		calls++
		if cliops.SourceCallID(ctx) != "actual-original-call" {
			t.Errorf("source call ID=%q", cliops.SourceCallID(ctx))
		}
		return actual, sourceErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		reply   cliops.CompactReply
		problem error
	}{
		{cliops.CompactReply{Text: "compacted 1-12"}, nil},
		{cliops.CompactReply{}, errors.New("actual state save failed after model usage")},
		{cliops.CompactReply{Error: "original source context rejected"}, nil},
	} {
		actual, sourceErr = tc.reply, tc.problem
		want := actual
		if sourceErr != nil {
			want.Error = sourceErr.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"--self", "--from=1", "--to=12"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runCompactRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (want.Error != "") || out.Len() == 0 || out.Len() > 32<<10 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("facts missing before failure output=%q err=%v", out.String(), err)
			}
			if asJSON {
				var got cliops.CompactReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || got != want {
					t.Fatalf("JSON facts=%+v want=%+v err=%v", got, want, err)
				}
			} else if out.String() != cliops.FormatCompact(want) {
				t.Fatalf("human facts=%q want=%q", out.String(), cliops.FormatCompact(want))
			}
			if want.Error != "" && strings.Contains(out.String(), "compacted") {
				t.Fatalf("actual error received fabricated acknowledgement: %q", out.String())
			}
		}
	}
	actual, sourceErr = cliops.CompactReply{}, errors.New("actual state save failure: "+strings.Repeat("\x00<&>界", 12<<10))
	var display cliops.CompactReply
	for _, asJSON := range []bool{true, false} {
		args := []string{"--self", "--from=1", "--to=12"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		err := runCompactRuntime(context.Background(), "", false, args, nil, &out)
		if err == nil || out.Len() == 0 || out.Len() > 32<<10 {
			t.Fatalf("large error output bytes=%d err=%v", out.Len(), err)
		}
		if asJSON {
			if err := json.Unmarshal(out.Bytes(), &display); err != nil || display.Text != "" || display.Error == "" || !strings.HasPrefix(display.Error, "actual state save failure: ") || !strings.Contains(display.Error, "[truncated]") {
				t.Fatalf("large error facts text=%q error bytes=%d err=%v", display.Text, len(display.Error), err)
			}
		} else if out.String() != cliops.FormatCompact(display) {
			t.Fatal("human and JSON error facts differ")
		}
	}
	if calls != 8 {
		t.Fatalf("source calls=%d want=8 without retry", calls)
	}
	// A malformed peer must not make either CLI mode print a clipped Text.
	peerCalls := 0
	body, err := json.Marshal(cliops.CompactReply{Text: strings.Repeat("x", (32<<10)+1)})
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerCalls++
		_, _ = w.Write(body)
	}))
	defer peer.Close()
	t.Setenv("XBOT_ENDPOINT", peer.URL)
	for _, args := range [][]string{{"--self", "--from=1", "--to=12"}, {"--self", "--from=1", "--to=12", "--json"}} {
		var out bytes.Buffer
		if err := runCompactRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("oversized Text printed bytes=%d err=%v", out.Len(), err)
		}
	}
	if peerCalls != 2 {
		t.Fatalf("malformed peer calls=%d want=2", peerCalls)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("CLI wrote offline state entries=%v err=%v", entries, err)
	}
}
