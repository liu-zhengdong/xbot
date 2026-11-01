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
	"xbot/internal/tools"
)

func TestPersonaRuntimeEarlyMainHelpAndUnavailable(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, args := range [][]string{
		{"persona", "--help"}, {"persona", "set", "--help"},
		{"persona", "set", "--file", filepath.Join(absent, "missing"), "--help"},
	} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent}, args...)) })
		if !strings.Contains(output, "persona set --self") || !strings.Contains(output, "--file") {
			t.Fatalf("incomplete help: %q", output)
		}
	}
	for _, args := range [][]string{
		{"persona"}, {"persona", "set", "--text=x"},
		{"persona", "set", "target", "--self", "--text=x"},
		{"persona", "set", "--self", "--file", filepath.Join(absent, "missing")},
	} {
		if err := run(append([]string{"--root", absent}, args...)); err == nil {
			t.Errorf("accepted unavailable/invalid persona: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	var out bytes.Buffer
	args := []string{"persona", "set", "--self", "--file", filepath.Join(absent, "missing")}
	if err := runBotRuntime(context.Background(), "", false, args, nil, &out); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("file read before missing endpoint error=%v", err)
	}
	t.Setenv("XBOT_ENDPOINT", "http://127.0.0.1:1")
	if err := runBotRuntime(context.Background(), t.TempDir(), true, args, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("file read before root mismatch error=%v", err)
	}
	if err := runBotRuntime(context.Background(), "", true, args, nil, &out); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root error=%v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unavailable command output=%q", out.String())
	}
}

func TestPersonaRuntimeFileStdinFullBytesAndReceipts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "original-source-call")
	var want string
	var operationErr error
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Bots: &cliops.BotHandlers{Persona: func(ctx context.Context, input cliops.BotPersona) (cliops.BotPersonaReply, error) {
		calls++
		if input.Text != want || cliops.SourceCallID(ctx) != "original-source-call" {
			t.Errorf("raw text bytes=%d want=%d call=%q", len(input.Text), len(want), cliops.SourceCallID(ctx))
		}
		if operationErr != nil {
			return cliops.BotPersonaReply{Name: "actual-nonprimary"}, operationErr
		}
		return cliops.BotPersonaReply{Name: "actual-nonprimary", Written: true, Effective: "next_wake"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	// Both inputs are below the original domain's limit after trim. Unicode
	// reaches exactly 32768 UTF-8 bytes; the raw padded input exceeds 32 KiB.
	for _, text := range []string{strings.Repeat("x", 32<<10), " \n" + strings.Repeat("界", 10922) + "ab\n\t "} {
		want = text
		for _, mode := range []string{"file", "stdin"} {
			file := "-"
			var stdin io.Reader = strings.NewReader(text)
			if mode == "file" {
				file = filepath.Join(t.TempDir(), "duties.txt")
				if err := os.WriteFile(file, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				stdin = nil
			}
			for _, asJSON := range []bool{false, true} {
				args := []string{"persona", "set", "--self", "--file", file}
				if asJSON {
					args = append(args, "--json")
				}
				if mode == "stdin" {
					stdin = strings.NewReader(text)
				}
				var out bytes.Buffer
				if err := runBotRuntime(context.Background(), "", false, args, stdin, &out); err != nil {
					t.Fatal(err)
				}
				reply := cliops.BotPersonaReply{Name: "actual-nonprimary", Written: true, Effective: "next_wake"}
				expected := cliops.FormatBotPersona(reply)
				if asJSON {
					raw, _ := json.Marshal(reply)
					expected = string(raw) + "\n"
				}
				if out.String() != expected {
					t.Fatalf("%s JSON=%t output=%q", mode, asJSON, out.String())
				}
			}
		}
	}
	want = "small raw 职责"
	output := captureStdout(t, func() error {
		return run([]string{"--root", root, "persona", "set", "--self", "--text", want, "--json"})
	})
	if !strings.Contains(output, `"written":true`) {
		t.Fatalf("early main output=%q", output)
	}
	operationErr = errors.New("domain write failed")
	for _, asJSON := range []bool{false, true} {
		args := []string{"persona", "set", "--self", "--text", want}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		err := runBotRuntime(context.Background(), "", false, args, nil, &out)
		if err == nil || !strings.Contains(out.String(), "domain write failed") || strings.Contains(out.String(), "next_wake") {
			t.Fatalf("failed write JSON=%t output=%q error=%v", asJSON, out.String(), err)
		}
		if asJSON {
			var reply cliops.BotPersonaReply
			if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.Written {
				t.Fatalf("failed ACK=%q decode=%v", out.String(), err)
			}
		} else if !strings.Contains(out.String(), "written: false\n") {
			t.Fatalf("failed human ACK=%q", out.String())
		}
	}
	if calls != 11 {
		t.Fatalf("calls=%d want 11 without retry", calls)
	}
}

func TestPersonaRuntimeInputMaxAndEscaping(t *testing.T) {
	// Exact final wire budget and max+1 differ by one byte. The raw reader also
	// has its own cap, independent of each caller's typed JSON envelope.
	base, _ := json.Marshal(cliops.BotPersona{})
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{strings.Repeat("x", cliops.BotMaxRequest-len(base)), true},
		{strings.Repeat("x", cliops.BotMaxRequest-len(base)+1), false},
		{strings.Repeat("x", cliops.BotMaxRequest), false},
		{strings.Repeat("x", cliops.BotMaxRequest+1), false},
		{strings.Repeat("<", cliops.BotMaxRequest/6+1), false},
		{" \n" + strings.Repeat("界", 10922) + "ab\n ", true},
		{"", true}, // Empty and whitespace duties are domain validation.
		{" \n ", true},
	} {
		for _, mode := range []string{"text", "stdin", "file"} {
			command := cliops.BotCommand{Persona: tc.text}
			var stdin io.Reader
			if mode != "text" {
				command.Persona = ""
				command.PersonaFile = "-"
				stdin = strings.NewReader(tc.text)
				if mode == "file" {
					command.PersonaFile = filepath.Join(t.TempDir(), "input.txt")
					if err := os.WriteFile(command.PersonaFile, []byte(tc.text), 0600); err != nil {
						t.Fatal(err)
					}
					stdin = nil
				}
			}
			input, err := readBotPersonaInput(command, stdin)
			if (err == nil) != tc.valid || (tc.valid && input.Text != tc.text) {
				t.Fatalf("mode=%s bytes=%d valid=%t got=%d error=%v", mode, len(tc.text), tc.valid, len(input.Text), err)
			}
		}
	}
	reader := &personaRuntimeCountingReader{remaining: 2 * cliops.BotMaxRequest}
	_, err := readBotPersonaInput(cliops.BotCommand{PersonaFile: "-"}, reader)
	if err == nil || reader.read != cliops.BotMaxRequest+1 {
		t.Fatalf("max+1 read=%d error=%v", reader.read, err)
	}
}

type personaRuntimeCountingReader struct{ remaining, read int }

func (r *personaRuntimeCountingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return n, nil
}

func TestPersonaRuntimeCLIExactBudgetsAndWrittenFacts(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	var body []byte
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(body)
	}))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	reply := cliops.BotPersonaReply{Written: true, Effective: "next_wake"}
	base, _ := json.Marshal(reply)
	reply.Name = strings.Repeat("x", tools.DefaultShellOutputBytes-len(base)-1)
	for _, asJSON := range []bool{false, true} {
		body, _ = json.Marshal(reply)
		args := []string{"persona", "set", "--self", "--text=x"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := runBotRuntime(context.Background(), "", false, args, nil, &out); err != nil {
			t.Fatal(err)
		}
		expected := cliops.FormatBotPersona(reply)
		if asJSON {
			expected = string(body) + "\n"
		}
		if out.String() != expected || out.Len() > tools.DefaultShellOutputBytes || (asJSON && out.Len() != tools.DefaultShellOutputBytes) {
			t.Fatalf("exact JSON=%t bytes=%d", asJSON, out.Len())
		}
	}
	for _, value := range []cliops.BotPersonaReply{
		{Name: reply.Name + "x", Written: true, Effective: "next_wake"},
		{Name: strings.Repeat(`"`, 17000), Written: true, Effective: "next_wake"},
		{Name: "actual", Written: true, Effective: "next_wake", Error: strings.Repeat("e", 33<<10)},
	} {
		for _, asJSON := range []bool{false, true} {
			body, _ = json.Marshal(value)
			args := []string{"persona", "set", "--self", "--text=x"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runBotRuntime(context.Background(), "", false, args, nil, &out)
			if err == nil || out.Len() == 0 || out.Len() > tools.DefaultShellOutputBytes || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("overflow JSON=%t bytes=%d error=%v", asJSON, out.Len(), err)
			}
			if asJSON {
				var got cliops.BotPersonaReply
				if decode := json.Unmarshal(out.Bytes(), &got); decode != nil || !got.Written || got.Effective != "next_wake" || got.Error == "" {
					t.Fatalf("facts lost decode=%v written=%t effective=%q", decode, got.Written, got.Effective)
				}
			} else if !strings.Contains(out.String(), "written: true\neffective: next_wake\nerror:") {
				t.Fatalf("human facts lost output bytes=%d", out.Len())
			}
		}
	}
	if requests != 8 {
		t.Fatalf("requests=%d want 8 without retry", requests)
	}
}
