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

func TestPeerMessageRuntimeEarlyHelpAndRoot(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, args := range [][]string{{"--help"}, {"-h"}, {"B", "--file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent, "peer-message"}, args...)) })
		if !strings.Contains(output, "peer-message TARGET --self") || !strings.Contains(output, "Help works offline") {
			t.Fatalf("help=%q", output)
		}
	}
	for _, args := range [][]string{{"B", "--self", "--text=x"}, {"B", "--self", "--file=-"}, {"B", "--text=x"}, {"send", "B", "--self", "--text=x"}} {
		if err := run(append([]string{"--root", absent, "peer-message"}, args...)); err == nil {
			t.Fatalf("runtime succeeded without source: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized data root: %v", err)
	}
	t.Setenv("XBOT_ROOT", t.TempDir())
	if err := run([]string{"--root=", "peer-message", "B", "--self", "--text=x"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root error=%v", err)
	}
	var out bytes.Buffer
	if err := runPeerMessageRuntime(context.Background(), t.TempDir(), true, []string{"B", "--self", "--text=x"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || out.Len() != 0 {
		t.Fatalf("mismatch out=%q err=%v", out.String(), err)
	}
}

func TestPeerMessageRuntimeCLIFileStdinAndSourceIdentity(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "stale-diagnostic-must-not-select-source")
	t.Setenv("XBOT_CALL_ID", "original-prepared-call")
	text := "\n" + strings.Repeat("界", 2730) + "\n" // exactly 8 KiB, including surrounding whitespace
	file := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{PeerMessage: func(ctx context.Context, input cliops.PeerMessage) (cliops.PeerMessageReply, error) {
		calls++
		if input.Bot != " B " || input.Text != text || cliops.SourceCallID(ctx) != "original-prepared-call" {
			t.Errorf("bot=%q text bytes=%d source call=%q", input.Bot, len(input.Text), cliops.SourceCallID(ctx))
		}
		return cliops.PeerMessageReply{Bot: "B", Route: "local", Posted: true, EventID: "event-actual", Stage: "complete"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	want := cliops.PeerMessageReply{Bot: "B", Route: "local", Posted: true, EventID: "event-actual", Stage: "complete"}
	for _, tc := range []struct {
		args  []string
		stdin io.Reader
		json  bool
	}{
		{[]string{" B ", "--self", "--file", file, "--json"}, nil, true},
		{[]string{" B ", "--self", "--file=-"}, strings.NewReader(text), false},
	} {
		var out bytes.Buffer
		if err := runPeerMessageRuntime(context.Background(), root, true, tc.args, tc.stdin, &out); err != nil {
			t.Fatal(err)
		}
		if tc.json {
			var got cliops.PeerMessageReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("reply=%s err=%v", out.String(), err)
			}
		} else if out.String() != cliops.FormatPeerMessage(want) {
			t.Fatalf("human output=%q", out.String())
		}
		if out.Len() > 1024 || strings.Contains(out.String(), "界") {
			t.Fatalf("full body appeared in short receipt: bytes=%d", out.Len())
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2", calls)
	}
}

func TestPeerMessageRuntimeLiteralFlagsAreData(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "original")
	var got cliops.PeerMessage
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{PeerMessage: func(_ context.Context, input cliops.PeerMessage) (cliops.PeerMessageReply, error) {
		calls++
		got = input
		return cliops.PeerMessageReply{Route: "local", Stage: "validation"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		args []string
		want cliops.PeerMessage
	}{
		{[]string{"B", "--self", "--text", "--help"}, cliops.PeerMessage{Bot: "B", Text: "--help"}},
		{[]string{"B", "--text", "--self", "--self"}, cliops.PeerMessage{Bot: "B", Text: "--self"}},
		{[]string{"--self", "--text=x", "--", "--help"}, cliops.PeerMessage{Bot: "--help", Text: "x"}},
	} {
		var out bytes.Buffer
		if err := runPeerMessageRuntime(context.Background(), "", false, tc.args, nil, &out); err != nil || got != tc.want || strings.Contains(out.String(), "Usage:") {
			t.Fatalf("args=%q got=%+v want=%+v err=%v", tc.args, got, tc.want, err)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d want=3", calls)
	}
}

func TestPeerMessageRuntimeRawAndEscapedInputBudgets(t *testing.T) {
	text := " \n" + strings.Repeat("\x00<&>\"", 2048) + "\n "
	file := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.PeerMessageCommand{{Bot: "B", Text: text}, {Bot: "B", TextFile: file}, {Bot: "B", TextFile: "-"}} {
		input, err := readPeerMessageInput(command, strings.NewReader(text))
		if err != nil || input.Bot != "B" || input.Text != text {
			t.Fatalf("raw was changed: bytes=%d err=%v", len(input.Text), err)
		}
	}
	for _, tc := range []struct {
		command cliops.PeerMessageCommand
		stdin   string
	}{
		{cliops.PeerMessageCommand{Bot: "B", Text: strings.Repeat("x", cliops.PeerMessageMaxRequest+1)}, ""},
		{cliops.PeerMessageCommand{Bot: "B", TextFile: "-"}, strings.Repeat("x", cliops.PeerMessageMaxRequest+1)},
		{cliops.PeerMessageCommand{Bot: "B", Text: strings.Repeat("<", cliops.PeerMessageMaxRequest/6+1)}, ""},
		{cliops.PeerMessageCommand{Bot: strings.Repeat("<", cliops.PeerMessageMaxRequest/6+1), Text: "x"}, ""},
	} {
		if _, err := readPeerMessageInput(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatalf("oversized input accepted: target bytes=%d text bytes=%d stdin bytes=%d", len(tc.command.Bot), len(tc.command.Text), len(tc.stdin))
		}
	}
	base := `{"bot":"B","text":""}`
	exact := strings.Repeat("x", cliops.PeerMessageMaxRequest-len(base))
	if _, err := readPeerMessageInput(cliops.PeerMessageCommand{Bot: "B", Text: exact}, nil); err != nil {
		t.Fatalf("exact encoded limit: %v", err)
	}
	if _, err := readPeerMessageInput(cliops.PeerMessageCommand{Bot: "B", Text: exact + "x"}, nil); err == nil {
		t.Fatal("encoded max+1 accepted")
	}
	reader := &peerMessageRuntimeCountingReader{reader: strings.NewReader(strings.Repeat("x", 2*cliops.PeerMessageMaxRequest))}
	if _, err := readPeerMessageInput(cliops.PeerMessageCommand{Bot: "B", TextFile: "-"}, reader); err == nil || reader.bytes != cliops.PeerMessageMaxRequest+1 {
		t.Fatalf("bounded read consumed=%d err=%v", reader.bytes, err)
	}
}

type peerMessageRuntimeCountingReader struct {
	reader io.Reader
	bytes  int
}

func (reader *peerMessageRuntimeCountingReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	reader.bytes += n
	return n, err
}

func TestPeerMessageRuntimeErrorReceiptsPrintBeforeReturn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	t.Setenv("XBOT_CALL_ID", "original")
	var actual cliops.PeerMessageReply
	var sourceErr error
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{PeerMessage: func(context.Context, cliops.PeerMessage) (cliops.PeerMessageReply, error) {
		calls++
		return actual, sourceErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	recorded, notRecorded := true, false
	for _, tc := range []struct {
		reply cliops.PeerMessageReply
		err   error
	}{
		{cliops.PeerMessageReply{Bot: "B", Route: "local", Posted: true, EventID: "event-actual", Stage: "recipient_post"}, errors.New("late error")},
		{cliops.PeerMessageReply{Bot: "B@Laptop", Route: "remote", EnvelopeID: "env-actual", Gateway: "accepted", Stage: "record", Recorded: &notRecorded}, errors.New("record failed")},
		{cliops.PeerMessageReply{Route: "remote", EnvelopeID: "env-actual", Gateway: "unknown", Queued: true, Stage: "record", Recorded: &notRecorded}, errors.New("record failed after queue")},
		{cliops.PeerMessageReply{Route: "remote", EnvelopeID: "env-actual", Gateway: "http_error", GatewayStatus: 400, Stage: "gateway", Recorded: &recorded, RecordSeq: 123}, errors.New("gateway rejected")},
		{cliops.PeerMessageReply{Route: "remote", EnvelopeID: "env-actual", Gateway: "unknown", Stage: "queue", Recorded: &recorded, RecordSeq: 124}, errors.New("queue failed")},
		{cliops.PeerMessageReply{Route: "remote", EnvelopeID: "env-actual", Gateway: "unknown", Queued: true, Stage: "complete", Recorded: &recorded, RecordSeq: 125}, nil},
	} {
		actual, sourceErr = tc.reply, tc.err
		if tc.err != nil {
			tc.reply.Error = tc.err.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"B", "--self", "--text=x"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runPeerMessageRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (tc.err != nil) || out.Len() == 0 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("out=%q err=%v wantErr=%v", out.String(), err, tc.err)
			}
			if asJSON {
				var got cliops.PeerMessageReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, tc.reply) {
					t.Fatalf("got=%+v want=%+v decode=%v", got, tc.reply, err)
				}
			} else if out.String() != cliops.FormatPeerMessage(tc.reply) {
				t.Fatalf("human receipt changed: %q", out.String())
			}
		}
	}
	if calls != 12 {
		t.Fatalf("calls=%d want=12 without retries", calls)
	}
}

func TestPeerMessageRuntimeOversizedDisplayPreservesFacts(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	recorded := false
	actual := cliops.PeerMessageReply{Bot: strings.Repeat("\x00", 8<<10), Route: "remote", Posted: true, EventID: "event-actual", EnvelopeID: "env-actual", Gateway: "unknown", GatewayStatus: 503, Queued: true, Stage: "record", Recorded: &recorded, RecordSeq: 456, Error: strings.Repeat("\x01", 8<<10)}
	body, err := json.Marshal(actual)
	if err != nil || len(body) <= 32<<10 {
		t.Fatalf("fixture bytes=%d err=%v", len(body), err)
	}
	// Keep the raw HTTP response below 64 KiB while each display field can
	// independently overflow Shell's 32 KiB after JSON/string escaping.
	actual.Error = "late display failure"
	body, _ = json.Marshal(actual)
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.URL)
	for _, asJSON := range []bool{false, true} {
		args := []string{"B", "--self", "--text=x"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := runPeerMessageRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() == 0 || out.Len() > 32<<10 {
			t.Fatalf("bytes=%d err=%v", out.Len(), err)
		}
		if asJSON {
			var got cliops.PeerMessageReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := actual
			want.Bot, want.Error = got.Bot, got.Error
			if got.Bot != "" || got.Error == "" || !reflect.DeepEqual(got, want) {
				t.Fatalf("facts lost: got=%+v want=%+v", got, want)
			}
		} else {
			for _, fact := range []string{"posted: true\n", "event_id: event-actual\n", "envelope_id: env-actual\n", "gateway: unknown\n", "gateway_status: 503\n", "queued: true\n", "recorded: false\n", "record_seq: 456\n"} {
				if !strings.Contains(out.String(), fact) {
					t.Errorf("human receipt lost %q", fact)
				}
			}
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2 without retries", calls)
	}
}
