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

func TestExperienceRuntimeEarlyOfflineHelpAndNoFallthrough(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	for _, args := range [][]string{{"--help"}, {"record", "--help"}, {"search", "--help"}, {"record", "--summary-file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error { return run(append([]string{"--root", absent, "experience"}, args...)) })
		if !strings.Contains(output, "experience record --self") || !strings.Contains(output, "Help works offline") {
			t.Fatalf("help=%q", output)
		}
	}
	for _, args := range [][]string{{"record", "--self", "--summary=x"}, {"search", "--self"}, {"record", "--summary=x"}, {"record", "--self", "--summary", "--help"}, {"search", "--self", "--query", "--help"}, {"record", "--self", "--summary=x", "positional"}} {
		if err := run(append([]string{"--root", absent, "experience"}, args...)); err == nil {
			t.Fatalf("no source args succeeded: %q", args)
		}
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatalf("help/error initialized root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	if err := run([]string{"--root=", "experience", "search", "--self"}); err == nil || !strings.Contains(err.Error(), "explicit --root") {
		t.Fatalf("explicit empty root error=%v", err)
	}
	for _, endpoint := range []string{"", "http://localhost:1", "http://127.0.0.1:1"} {
		t.Setenv("XBOT_ENDPOINT", endpoint)
		for _, args := range [][]string{{"record", "--self", "--summary=x"}, {"search", "--self"}} {
			var out bytes.Buffer
			if err := runExperienceRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() != 0 {
				t.Fatalf("endpoint=%q out=%q err=%v", endpoint, out.String(), err)
			}
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failure initialized offline files: entries=%v err=%v", entries, err)
	}
}

func TestExperienceRuntimeCanonicalSourceFileStdinAndEarlyEnvironment(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_BOT", "stale-diagnostic-name")
	t.Setenv("XBOT_CALL_ID", "stale-invalid-call-id!")
	if err := os.WriteFile(filepath.Join(root, "env"), []byte("XBOT_ENDPOINT=http://127.0.0.1:1\nXBOT_ROOT=/absent\n"), 0600); err != nil {
		t.Fatal(err)
	}
	summary := " \n" + strings.Repeat(" ", 20<<10) + strings.Repeat("界", 682) + "ab\n " // original domain trims to exactly 2048 bytes
	focus := " \n" + strings.Repeat("focus界", 4000) + "\n "
	file := filepath.Join(t.TempDir(), "focus")
	if err := os.WriteFile(file, []byte(focus), 0600); err != nil {
		t.Fatal(err)
	}
	var gotRecord cliops.ExperienceRecord
	var gotSearch cliops.ExperienceSearch
	calls := 0
	confirmed := true
	recordReply := cliops.ExperienceRecordReply{ID: 7, From: 12, To: 15, Recorded: &confirmed}
	source, err := cliops.Start(root, cliops.Handlers{Experiences: &cliops.ExperienceHandlers{
		Record: func(ctx context.Context, input cliops.ExperienceRecord) (cliops.ExperienceRecordReply, error) {
			calls++
			gotRecord = input
			if cliops.SourceCallID(ctx) != "" {
				t.Error("experience requires an execution ID")
			}
			return recordReply, nil
		},
		Search: func(ctx context.Context, input cliops.ExperienceSearch) (cliops.ExperienceSearchReply, error) {
			calls++
			gotSearch = input
			return cliops.ExperienceSearchReply{Text: "original rendered semantic result"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	var out bytes.Buffer
	if err := runExperienceRuntime(context.Background(), root, true, []string{"record", "--self", "--summary-file=-", "--focus-file", file, "--from=0", "--to=-1", "--json"}, strings.NewReader(summary), &out); err != nil {
		t.Fatal(err)
	}
	if gotRecord != (cliops.ExperienceRecord{Summary: summary, Focus: focus, To: -1}) {
		t.Fatalf("input trimmed/capped: summary=%d focus=%d range=%d/%d", len(gotRecord.Summary), len(gotRecord.Focus), gotRecord.From, gotRecord.To)
	}
	var reply cliops.ExperienceRecordReply
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil || !reflect.DeepEqual(reply, recordReply) || !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("reply=%s err=%v", out.String(), err)
	}
	// run must dispatch before the offline root env loader and config initialization.
	output := captureStdout(t, func() error {
		return run([]string{"--root", root, "experience", "record", "--self", "--summary", "--help", "--focus", "--"})
	})
	if output != cliops.FormatExperienceRecord(recordReply) || gotRecord != (cliops.ExperienceRecord{Summary: "--help", Focus: "--"}) || os.Getenv("XBOT_ENDPOINT") != source.Endpoint {
		t.Fatalf("early dispatch output=%q record=%+v", output, gotRecord)
	}
	query := strings.Repeat("deployment ", 100) // deliberately greater than the Web query limit
	for _, limit := range []string{"0", "-5", "9"} {
		out.Reset()
		if err := runExperienceRuntime(context.Background(), "", false, []string{"search", "--self", "--query-file=-", "--limit", limit}, strings.NewReader(query), &out); err != nil {
			t.Fatal(err)
		}
		if gotSearch.Query != query || out.String() != "original rendered semantic result\n" {
			t.Fatalf("query/output changed: query bytes=%d output=%q", len(gotSearch.Query), out.String())
		}
	}
	if calls != 5 {
		t.Fatalf("calls=%d want=5", calls)
	}
	out.Reset()
	if err := runExperienceRuntime(context.Background(), t.TempDir(), true, []string{"search", "--self"}, nil, &out); err == nil || !strings.Contains(err.Error(), "mismatch") || out.Len() != 0 {
		t.Fatalf("root mismatch err=%v out=%q", err, out.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("offline state created: entries=%v err=%v", entries, err)
	}
}

func TestExperienceRuntimeInputPreservesDomainBytes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input")
	for _, summary := range []string{strings.Repeat("x", 2048), strings.Repeat("x", 2049), " \n" + strings.Repeat(" ", 20<<10) + " trimmed \n", strings.Repeat("界", 682) + "ab", "", " \n\t"} {
		if err := os.WriteFile(file, []byte(summary), 0600); err != nil {
			t.Fatal(err)
		}
		for _, command := range []cliops.ExperienceCommand{{Summary: summary}, {SummaryFile: file}, {SummaryFile: "-"}} {
			got, err := readExperienceRecordInput(command, strings.NewReader(summary))
			if err != nil || got.Summary != summary {
				t.Fatalf("summary bytes=%d became=%d err=%v", len(summary), len(got.Summary), err)
			}
		}
	}
	focus := strings.Repeat("界", 7000)
	if err := os.WriteFile(file, []byte(focus), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.ExperienceCommand{{Summary: "x", Focus: focus}, {Summary: "x", FocusFile: file}, {Summary: "x", FocusFile: "-"}} {
		got, err := readExperienceRecordInput(command, strings.NewReader(focus))
		if err != nil || got.Focus != focus {
			t.Fatalf("focus bytes=%d became=%d err=%v", len(focus), len(got.Focus), err)
		}
	}
	for _, query := range []string{"", " \n\t ", strings.Repeat("q", 513), "--help", "--"} {
		if err := os.WriteFile(file, []byte(query), 0600); err != nil {
			t.Fatal(err)
		}
		for _, command := range []cliops.ExperienceCommand{{Query: query}, {QueryFile: file}, {QueryFile: "-"}} {
			got, err := readExperienceSearchInput(command, strings.NewReader(query))
			if err != nil || got.Query != query {
				t.Fatalf("query bytes=%d changed err=%v", len(query), err)
			}
		}
	}
}

func TestExperienceRuntimeRawAndEscapedInputBudgets(t *testing.T) {
	max := cliops.ExperienceMaxRequest
	for _, tc := range []struct {
		command cliops.ExperienceCommand
		stdin   string
	}{
		{cliops.ExperienceCommand{Summary: strings.Repeat("x", max+1)}, ""},
		{cliops.ExperienceCommand{SummaryFile: "-"}, strings.Repeat("x", max+1)},
		{cliops.ExperienceCommand{Summary: "x", Focus: strings.Repeat("x", max+1)}, ""},
		{cliops.ExperienceCommand{Summary: "x", FocusFile: "-"}, strings.Repeat("x", max+1)},
		{cliops.ExperienceCommand{Summary: strings.Repeat("<", max/6+1)}, ""},
		{cliops.ExperienceCommand{Summary: "x", Focus: strings.Repeat("\x00", max/6+1)}, ""},
	} {
		if _, err := readExperienceRecordInput(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatalf("oversized record accepted: summary=%d focus=%d stdin=%d", len(tc.command.Summary), len(tc.command.Focus), len(tc.stdin))
		}
	}
	for _, tc := range []struct {
		command cliops.ExperienceCommand
		stdin   string
	}{
		{cliops.ExperienceCommand{Query: strings.Repeat("x", max+1)}, ""},
		{cliops.ExperienceCommand{QueryFile: "-"}, strings.Repeat("x", max+1)},
		{cliops.ExperienceCommand{Query: strings.Repeat("<", max/6+1)}, ""},
	} {
		if _, err := readExperienceSearchInput(tc.command, strings.NewReader(tc.stdin)); err == nil {
			t.Fatal("oversized query accepted")
		}
	}
	for _, field := range []string{"summary", "focus", "query"} {
		reader := &experienceRuntimeCountingReader{reader: strings.NewReader(strings.Repeat("x", 2*max))}
		var err error
		switch field {
		case "summary":
			_, err = readExperienceRecordInput(cliops.ExperienceCommand{SummaryFile: "-"}, reader)
		case "focus":
			_, err = readExperienceRecordInput(cliops.ExperienceCommand{Summary: "x", FocusFile: "-"}, reader)
		case "query":
			_, err = readExperienceSearchInput(cliops.ExperienceCommand{QueryFile: "-"}, reader)
		}
		if err == nil || reader.bytes != max+1 {
			t.Fatalf("%s consumed=%d err=%v", field, reader.bytes, err)
		}
	}
	for _, action := range []string{"record", "search"} {
		base := `{"summary":""}`
		if action == "search" {
			base = `{"query":""}`
		}
		exact := strings.Repeat("x", max-len(base))
		for _, text := range []string{exact, exact + "x"} {
			var err error
			if action == "record" {
				_, err = readExperienceRecordInput(cliops.ExperienceCommand{Summary: text}, nil)
			} else {
				_, err = readExperienceSearchInput(cliops.ExperienceCommand{Query: text}, nil)
			}
			if (err == nil) != (text == exact) {
				t.Fatalf("%s encoded bytes=%d err=%v", action, len(text)+len(base), err)
			}
		}
	}
	file := filepath.Join(t.TempDir(), "oversized")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", max+1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []cliops.ExperienceCommand{{SummaryFile: file}, {Summary: "x", FocusFile: file}, {SummaryFile: filepath.Join(t.TempDir(), "missing")}} {
		if _, err := readExperienceRecordInput(command, nil); err == nil {
			t.Fatal("bad file accepted")
		}
	}
	if _, err := readExperienceSearchInput(cliops.ExperienceCommand{QueryFile: file}, nil); err == nil {
		t.Fatal("oversized query file accepted")
	}
}

type experienceRuntimeCountingReader struct {
	reader io.Reader
	bytes  int
}

func (reader *experienceRuntimeCountingReader) Read(p []byte) (int, error) {
	n, err := reader.reader.Read(p)
	reader.bytes += n
	return n, err
}

func TestExperienceRuntimePartialOutputBeforeFailureAndArtifactMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	var record cliops.ExperienceRecordReply
	var search cliops.ExperienceSearchReply
	var sourceErr error
	calls := 0
	confirmed := true
	source, err := cliops.Start(root, cliops.Handlers{Experiences: &cliops.ExperienceHandlers{
		Record: func(context.Context, cliops.ExperienceRecord) (cliops.ExperienceRecordReply, error) {
			calls++
			return record, sourceErr
		},
		Search: func(context.Context, cliops.ExperienceSearch) (cliops.ExperienceSearchReply, error) {
			calls++
			return search, sourceErr
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, tc := range []struct {
		reply cliops.ExperienceRecordReply
		err   error
	}{
		{cliops.ExperienceRecordReply{ID: 17, From: 4, To: 9}, errors.New("write outcome unconfirmed")},
		{cliops.ExperienceRecordReply{From: 4, To: 9}, errors.New("database open failed")},
		{cliops.ExperienceRecordReply{ID: 18, From: 4, To: 9, Recorded: &confirmed, Error: "later display failure"}, nil},
		{cliops.ExperienceRecordReply{ID: 19, Recorded: &confirmed}, nil},
	} {
		record, sourceErr = tc.reply, tc.err
		want := record
		if sourceErr != nil {
			want.Error = sourceErr.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"record", "--self", "--summary=x"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runExperienceRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (want.Error != "") || out.Len() == 0 || !strings.HasSuffix(out.String(), "\n") {
				t.Fatalf("record output=%q err=%v", out.String(), err)
			}
			if asJSON {
				var got cliops.ExperienceRecordReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("record got=%+v want=%+v err=%v", got, want, err)
				}
				if want.Recorded == nil && !strings.Contains(out.String(), `"recorded":null`) {
					t.Fatal("unconfirmed receipt lacks null")
				}
			} else if out.String() != cliops.FormatExperienceRecord(want) {
				t.Fatalf("human=%q", out.String())
			}
		}
	}
	// This path deliberately does not exist: the CLI must print metadata without
	// opening, expanding or replacing the source artifact.
	for _, tc := range []struct {
		reply cliops.ExperienceSearchReply
		err   error
	}{
		{cliops.ExperienceSearchReply{Text: "original semantic text"}, nil},
		{cliops.ExperienceSearchReply{File: "/current/results/nonexistent.json", Access: "local", Bytes: 36712}, nil},
		{cliops.ExperienceSearchReply{File: "/current/results/nonexistent.json", Access: "local", Bytes: 36712}, errors.New("late metadata failure")},
	} {
		search, sourceErr = tc.reply, tc.err
		want := search
		if sourceErr != nil {
			want.Error = sourceErr.Error()
		}
		for _, asJSON := range []bool{false, true} {
			args := []string{"search", "--self"}
			if asJSON {
				args = append(args, "--json")
			}
			var out bytes.Buffer
			err := runExperienceRuntime(context.Background(), "", false, args, nil, &out)
			if (err != nil) != (tc.err != nil) || out.Len() == 0 {
				t.Fatalf("search out=%q err=%v", out.String(), err)
			}
			if asJSON {
				var got cliops.ExperienceSearchReply
				if err := json.Unmarshal(out.Bytes(), &got); err != nil || got != want {
					t.Fatalf("metadata changed got=%+v want=%+v err=%v", got, want, err)
				}
			} else if out.String() != cliops.FormatExperienceSearch(want) {
				t.Fatalf("human metadata=%q", out.String())
			}
		}
	}
	if calls != 14 {
		t.Fatalf("calls=%d want=14 with no retries", calls)
	}
}

func TestExperienceRuntimeOversizedDisplayErrorPreservesAcknowledgement(t *testing.T) {
	t.Setenv("XBOT_ROOT", t.TempDir())
	confirmed := true
	actual := cliops.ExperienceRecordReply{ID: 9223372036854775807, From: 4, To: 19, Recorded: &confirmed, Error: strings.Repeat("\x00", 8<<10)}
	body, err := json.Marshal(actual)
	if err != nil || len(body) <= 32<<10 || len(body) > cliops.ExperienceMaxReply {
		t.Fatal("invalid display error fixture")
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
		args := []string{"record", "--self", "--summary=x"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := runExperienceRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() == 0 || out.Len() > 32<<10 {
			t.Fatalf("bytes=%d err=%v", out.Len(), err)
		}
		if asJSON {
			var got cliops.ExperienceRecordReply
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := actual
			want.Error = got.Error
			if !reflect.DeepEqual(got, want) || got.Error == "" {
				t.Fatalf("ack lost: got=%+v want=%+v", got, want)
			}
		} else if !strings.Contains(out.String(), "recorded experience #9223372036854775807 (seq 4-19)\n") {
			t.Fatalf("ack missing: %q", out.String())
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2", calls)
	}
}
