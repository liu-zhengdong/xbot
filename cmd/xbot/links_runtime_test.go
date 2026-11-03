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

func TestLinksRuntimeOfflineAndCanonicalBoundSource(t *testing.T) {
	t.Setenv("XBOT_ROOT", "")
	t.Setenv("XBOT_ENDPOINT", "")
	t.Setenv("XBOT_CALL_ID", "")
	absent := filepath.Join(t.TempDir(), "must-stay-absent")
	oldDefault := defaultCommand
	defaultCommand = "app"
	defer func() { defaultCommand = oldDefault }()
	for _, args := range [][]string{{"--help"}, {"search", "--help"}, {"load", "--help"}, {"search", "--query-file", filepath.Join(absent, "missing"), "--help"}} {
		output := captureStdout(t, func() error {
			return run(append([]string{"--root", absent, "links"}, args...))
		})
		if output != linksUsage {
			t.Fatalf("offline help=%q", output)
		}
	}
	for _, args := range [][]string{
		{"search", "--self"}, {"search"}, {"load", "--self", "browser"}, {"load", "browser"},
		{"load", "--self"}, {"install", "--self", "browser"}, {"search", "--self", "--query", "x", "extra"},
		{"-h"}, {"help"}, {"--self", "search"}, {"search", "--self", "--self"},
	} {
		if err := run(append([]string{"--root", absent, "links"}, args...)); err == nil {
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
	searchCalls, loadCalls := 0, 0
	var gotSearch cliops.LinksSearch
	var gotLoad cliops.LinksLoad
	source, err := cliops.Start(root, cliops.Handlers{Links: &cliops.LinksHandlers{
		Search: func(ctx context.Context, input cliops.LinksSearch) (cliops.LinksReply, error) {
			searchCalls++
			gotSearch = input
			if cliops.SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("search source call ID=%q", cliops.SourceCallID(ctx))
			}
			return cliops.LinksReply{Text: "browser\tavailable\tLocal Chrome"}, nil
		},
		Load: func(ctx context.Context, input cliops.LinksLoad) (cliops.LinksReply, error) {
			loadCalls++
			gotLoad = input
			if cliops.SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("load source call ID=%q", cliops.SourceCallID(ctx))
			}
			return cliops.LinksReply{Text: "loaded browser: browser_open"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	for _, explicitRoot := range []string{root, alias} {
		output := captureStdout(t, func() error {
			return run([]string{"--root", explicitRoot, "links", "search", "--self", "--query", "browser"})
		})
		if output != "browser\tavailable\tLocal Chrome\n" || gotSearch != (cliops.LinksSearch{Query: "browser"}) {
			t.Fatalf("early runtime dispatch output=%q input=%+v", output, gotSearch)
		}
	}
	output := captureStdout(t, func() error {
		return run([]string{"--root", alias, "links", "load", "--self", "browser", "--json"})
	})
	var decoded cliops.LinksReply
	if json.Unmarshal([]byte(output), &decoded) != nil || decoded.Text != "loaded browser: browser_open" || gotLoad != (cliops.LinksLoad{ID: "browser"}) {
		t.Fatalf("load output=%q input=%+v", output, gotLoad)
	}
	args := []string{"search", "--self"}
	for _, given := range []string{"", t.TempDir()} {
		var out bytes.Buffer
		err := runLinksRuntime(context.Background(), given, true, args, nil, &out)
		if err == nil || out.Len() != 0 || (!strings.Contains(err.Error(), "explicit --root") && !strings.Contains(err.Error(), "mismatch")) {
			t.Fatalf("explicit root=%q output=%q err=%v", given, out.String(), err)
		}
	}
	for _, endpoint := range []string{"", "http://localhost:1", "https://127.0.0.1:1", "http://127.0.0.1:1/path", "http://127.0.0.1:1"} {
		t.Setenv("XBOT_ENDPOINT", endpoint)
		var out bytes.Buffer
		if err := runLinksRuntime(context.Background(), "", false, args, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("endpoint=%q output=%q err=%v", endpoint, out.String(), err)
		}
	}
	t.Setenv("XBOT_ENDPOINT", source.Endpoint)
	t.Setenv("XBOT_CALL_ID", "")
	for _, callArgs := range [][]string{{"search", "--self"}, {"load", "--self", "browser"}} {
		var out bytes.Buffer
		if err := runLinksRuntime(context.Background(), "", false, callArgs, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("missing call ID output=%q err=%v", out.String(), err)
		}
	}
	t.Setenv("XBOT_CALL_ID", "actual-original-call")
	// Query from file and stdin each stay literal and bounded.
	queryFile := filepath.Join(t.TempDir(), "query.txt")
	if err := os.WriteFile(queryFile, []byte(" \n界\t "), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runLinksRuntime(context.Background(), "", false, []string{"search", "--self", "--query-file", queryFile}, nil, &out); err != nil || gotSearch != (cliops.LinksSearch{Query: " \n界\t "}) {
		t.Fatalf("query-file output=%q input=%+v err=%v", out.String(), gotSearch, err)
	}
	out.Reset()
	if err := runLinksRuntime(context.Background(), "", false, []string{"search", "--self", "--query-file", "-"}, strings.NewReader("stdin query"), &out); err != nil || gotSearch != (cliops.LinksSearch{Query: "stdin query"}) {
		t.Fatalf("stdin query output=%q input=%+v err=%v", out.String(), gotSearch, err)
	}
	for _, input := range []io.Reader{strings.NewReader(strings.Repeat("x", (1<<20)+1)), nil} {
		out.Reset()
		var err error
		if input == nil {
			err = runLinksRuntime(context.Background(), "", false, []string{"search", "--self", "--query", strings.Repeat("x", (1<<20)+1)}, nil, &out)
		} else {
			err = runLinksRuntime(context.Background(), "", false, []string{"search", "--self", "--query-file", "-"}, input, &out)
		}
		if err == nil || out.Len() != 0 {
			t.Fatalf("oversized query input output=%q err=%v", out.String(), err)
		}
	}
	before := searchCalls + loadCalls
	// Late source errors print received facts then fail.
	source.Close()
	peerCalls := 0
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cliops.LinksReply{Error: "actual load failure"})
	}))
	defer peer.Close()
	t.Setenv("XBOT_ENDPOINT", peer.URL)
	for _, callArgs := range [][]string{{"search", "--self"}, {"load", "--self", "browser"}} {
		out.Reset()
		err := runLinksRuntime(context.Background(), "", false, callArgs, nil, &out)
		if err == nil || err.Error() != "actual load failure" || out.String() != "error: actual load failure\n" {
			t.Fatalf("late error output=%q err=%v", out.String(), err)
		}
	}
	out.Reset()
	err = runLinksRuntime(context.Background(), "", false, []string{"search", "--self", "--json"}, nil, &out)
	var errorReply cliops.LinksReply
	if err == nil || err.Error() != "actual load failure" || json.Unmarshal(out.Bytes(), &errorReply) != nil || errorReply.Error != "actual load failure" {
		t.Fatalf("late JSON error output=%q err=%v", out.String(), err)
	}
	if peerCalls != 3 || searchCalls+loadCalls != before {
		t.Fatalf("peer calls=%d source calls=%d", peerCalls, searchCalls+loadCalls)
	}
	// Oversized Text never prints.
	body, err := json.Marshal(cliops.LinksReply{Text: strings.Repeat("x", (32<<10)+1)})
	if err != nil {
		t.Fatal(err)
	}
	peerCalls = 0
	bigPeer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerCalls++
		_, _ = w.Write(body)
	}))
	defer bigPeer.Close()
	t.Setenv("XBOT_ENDPOINT", bigPeer.URL)
	for _, callArgs := range [][]string{{"search", "--self"}, {"search", "--self", "--json"}} {
		out.Reset()
		if err := runLinksRuntime(context.Background(), "", false, callArgs, nil, &out); err == nil || out.Len() != 0 {
			t.Fatalf("oversized Text printed bytes=%d err=%v", out.Len(), err)
		}
	}
	if peerCalls != 2 {
		t.Fatalf("malformed peer calls=%d want=2", peerCalls)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "env" {
		t.Fatalf("CLI wrote offline state entries=%v err=%v", entries, err)
	}
}
