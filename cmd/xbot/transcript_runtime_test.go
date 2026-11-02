package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestTranscriptRuntimeEarlyOfflineHelpAndNoFallback(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "absent")
	oldDefault := defaultCommand
	defaultCommand = "app"
	defer func() { defaultCommand = oldDefault }()
	for _, args := range [][]string{{"--help"}, {"search", "--help"}, {"expand", "--help"}, {"detail", "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent, "transcript"}, args...)) })
		if !strings.Contains(output, "transcript search --self") {
			t.Fatalf("offline help missing: %q", output)
		}
	}
	if err := run([]string{"--root", absent, "transcript", "search", "--self"}); err == nil {
		t.Fatal("missing source succeeded")
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("offline entry initialized root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	var out bytes.Buffer
	if err := runTranscriptRuntime(context.Background(), "", false, []string{"search", "--self"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("missing endpoint output=%q err=%v", out.String(), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing endpoint initialized offline service: entries=%v err=%v", entries, err)
	}
}

func TestTranscriptRuntimeCanonicalSourceAndDomainInputs(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "stale-before-rename")
	t.Setenv("XBOT_CALL_ID", "foreign-finished-call")
	calls := 0
	var search cliops.TranscriptSearch
	var expand cliops.TranscriptExpand
	var detail cliops.TranscriptDetail
	source, err := cliops.Start(root, cliops.Handlers{Transcript: &cliops.TranscriptHandlers{
		Search: func(_ context.Context, in cliops.TranscriptSearch) (cliops.TranscriptReadReply, error) {
			calls++
			search = in
			return cliops.TranscriptReadReply{Text: "found seq 7"}, nil
		},
		Expand: func(_ context.Context, in cliops.TranscriptExpand) (cliops.TranscriptReadReply, error) {
			calls++
			expand = in
			return cliops.TranscriptReadReply{}, errors.New("original invalid range")
		},
		Detail: func(_ context.Context, in cliops.TranscriptDetail) (cliops.TranscriptReadReply, error) {
			calls++
			detail = in
			return cliops.TranscriptReadReply{}, errors.New("original invalid seq")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	output := captureStdout(t, func() error {
		return run([]string{"--root", root, "transcript", "search", "--self", "--query", "--help", "--bot", "--"})
	})
	if output != "found seq 7\n" || search != (cliops.TranscriptSearch{Query: "--help", Bot: "--"}) {
		t.Fatalf("early source dispatch output=%q input=%+v", output, search)
	}
	t.Setenv("XBOT_CALL_ID", "")
	for _, args := range [][]string{{"expand", "--self", "--from=-2", "--to=0"}, {"detail", "--self", "--seq=0", "--experience=-1"}} {
		var out bytes.Buffer
		if err := runTranscriptRuntime(context.Background(), root, true, args, nil, &out); err == nil || !strings.Contains(err.Error(), "original invalid") || out.Len() == 0 {
			t.Fatalf("domain error missing facts=%q err=%v", out.String(), err)
		}
	}
	if expand.From != -2 || expand.To != 0 || detail.Seq != 0 || detail.Experience != -1 || calls != 3 {
		t.Fatalf("domain inputs changed: expand=%+v detail=%+v calls=%d", expand, detail, calls)
	}
	for _, rawRoot := range []string{"", t.TempDir()} {
		var out bytes.Buffer
		if err := runTranscriptRuntime(context.Background(), rawRoot, true, []string{"search", "--self"}, nil, &out); err == nil || out.Len() != 0 || calls != 3 {
			t.Fatalf("bad explicit root reached source: err=%v calls=%d", err, calls)
		}
	}
}

func TestTranscriptRuntimeRawFileStdinAndEscapedBudgets(t *testing.T) {
	query := " \n\t原文<&>\n "
	file := filepath.Join(t.TempDir(), "query")
	if err := os.WriteFile(file, []byte(query), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.TranscriptCommand{{Query: query}, {QueryFile: file}, {QueryFile: "-"}} {
		command.Bot = " target "
		got, err := readTranscriptSearchInput(command, strings.NewReader(query))
		if err != nil || got.Query != query || got.Bot != command.Bot {
			t.Fatalf("input trimmed or changed: %+v err=%v", got, err)
		}
	}
	max := cliops.TranscriptMaxRequest
	reader := &transcriptCountingReader{reader: strings.NewReader(strings.Repeat("x", 2*max))}
	if _, err := readTranscriptSearchInput(cliops.TranscriptCommand{QueryFile: "-"}, reader); err == nil || reader.bytes != max+1 {
		t.Fatalf("stdin read=%d want=%d err=%v", reader.bytes, max+1, err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", max+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTranscriptSearchInput(cliops.TranscriptCommand{QueryFile: file}, nil); err == nil {
		t.Fatal("oversize file accepted")
	}
	for _, query := range []string{strings.Repeat("x", max+1), strings.Repeat("<", max/6+1)} {
		if _, err := readTranscriptSearchInput(cliops.TranscriptCommand{Query: query}, nil); err == nil {
			t.Fatal("oversize raw or escaped input accepted")
		}
	}
	base := `{"query":""}`
	exact := strings.Repeat("x", max-len(base))
	if got, err := readTranscriptSearchInput(cliops.TranscriptCommand{Query: exact}, nil); err != nil || got.Query != exact {
		t.Fatalf("exact escaped request rejected: err=%v", err)
	}
	if _, err := readTranscriptSearchInput(cliops.TranscriptCommand{Query: exact + "x"}, nil); err == nil {
		t.Fatal("escaped request over boundary accepted")
	}
}

type transcriptCountingReader struct {
	reader io.Reader
	bytes  int
}

func (r *transcriptCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += n
	return n, err
}

func TestTranscriptRuntimePrintsPartialFactsBeforeFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	var actual cliops.TranscriptReadReply
	calls := 0
	source, err := cliops.Start(root, cliops.Handlers{Transcript: &cliops.TranscriptHandlers{Search: func(context.Context, cliops.TranscriptSearch) (cliops.TranscriptReadReply, error) {
		calls++
		return actual, errors.New("source read failed")
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, facts := range []cliops.TranscriptReadReply{{Text: " full semantic text\n"}, {File: "results/nonexistent.json", Access: "local", Bytes: 65432}} {
		actual = facts
		want := facts
		want.Error = "source read failed"
		for _, jsonOutput := range []bool{false, true} {
			args := []string{"search", "--self"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runTranscriptRuntime(context.Background(), "", false, args, nil, &out)
			if err == nil || out.Len() == 0 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("facts absent before failure: out=%q err=%v", out.String(), err)
			}
			if jsonOutput {
				wire, _ := json.Marshal(want)
				if out.String() != string(wire)+"\n" {
					t.Fatalf("JSONNL facts changed: %q", out.String())
				}
			} else if out.String() != cliops.FormatTranscriptRead(want) {
				t.Fatalf("human facts changed: %q", out.String())
			}
		}
	}
	if calls != 4 {
		t.Fatalf("source calls=%d want=4 without retry", calls)
	}
}
