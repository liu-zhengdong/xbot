package notable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletedEffectsAndOrdinaryReads(t *testing.T) {
	work := t.TempDir()
	work, _ = filepath.EvalSymlinks(work)
	outside := t.TempDir()
	target := filepath.Join(outside, "result.txt")
	if err := os.WriteFile(target, []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	pip := filepath.Join(outside, "bin", "pip")
	if err := os.MkdirAll(filepath.Dir(pip), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pip, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, kind string
		run        func(context.Context)
	}{
		{"global npm", "global_install", func(c context.Context) { Shell(c, "npm i -g demo", work, nil) }},
		{"brew", "global_install", func(c context.Context) { Shell(c, "brew install demo", work, nil) }},
		{"go", "global_install", func(c context.Context) { Shell(c, "go install example.org/demo@latest", work, nil) }},
		{"pip outside venv", "global_install", func(c context.Context) { Shell(c, pip+" install demo", work, nil) }},
		{"pip venv", "", func(c context.Context) { Shell(c, pip+" install demo", work, []string{"VIRTUAL_ENV=/venv"}) }},
		{"pip local target", "", func(c context.Context) { Shell(c, pip+" install --target=./vendor demo", work, nil) }},
		{"dry run", "", func(c context.Context) { Shell(c, "npm install -g --dry-run demo", work, nil) }},
		{"write outside", "write_outside", func(c context.Context) { Wrote(c, target) }},
		{"write inside", "", func(c context.Context) { Wrote(c, filepath.Join(work, "a.txt")) }},
		{"read file outside", "", func(c context.Context) { Tool(c, "read_file", json.RawMessage(`{"path":"/outside"}`), "data", false) }},
		{"read shell", "", func(c context.Context) { Shell(c, "cat rm", work, nil) }},
		{"quoted names", "", func(c context.Context) { Shell(c, `echo 'rm -rf /tmp and npm install -g demo > /tmp/x'`, work, nil) }},
		{"curl body mentions rm", "external_message", func(c context.Context) { Shell(c, `curl -X POST --data rm https://example.invalid`, work, nil) }},
		{"git path named rm", "", func(c context.Context) { Shell(c, `git status rm`, work, nil) }},
		{"find name rm", "", func(c context.Context) { Shell(c, `find . -name rm`, work, nil) }},
		{"help is not install", "", func(c context.Context) { Shell(c, `brew install --help`, work, nil) }},
		{"shell home redirect", "write_outside", func(c context.Context) { Shell(c, "echo ok > ~/result.txt", work, []string{"HOME=" + outside}) }},
		{"quoted home is literal", "", func(c context.Context) { Shell(c, "echo ok > '~/result.txt'", work, []string{"HOME=" + outside}) }},
		{"shell redirect", "write_outside", func(c context.Context) { Shell(c, "echo ok > "+target, work, nil) }},
		{"unknown compound", "", func(c context.Context) { Shell(c, "false && npm install -g demo", work, nil) }},
		{"delete", "delete", func(c context.Context) { Tool(c, "delete_file", nil, "deleted", false) }},
		{"shell delete", "delete", func(c context.Context) { Shell(c, "rm one.txt", work, nil) }},
		{"send", "external_message", func(c context.Context) { Tool(c, "telegram_send", nil, "sent", false) }},
		{"read-only MCP", "", func(c context.Context) { Tool(c, "mcp_docs_get", nil, "data", true) }},
		{"MCP write", "external_message", func(c context.Context) { Tool(c, "mcp_docs_post", nil, "ok", false) }},
		{"links load", "", func(c context.Context) { Tool(c, "links", json.RawMessage(`{"action":"load"}`), "loaded", false) }},
		{"http GET", "", func(c context.Context) {
			Tool(c, "http_request", json.RawMessage(`{"method":"GET"}`), "200 OK\ndata", false)
		}},
		{"http failed", "", func(c context.Context) {
			Tool(c, "http_request", json.RawMessage(`{"method":"POST"}`), "500 failed", false)
		}},
		{"http POST", "external_message", func(c context.Context) {
			Tool(c, "http_request", json.RawMessage(`{"method":"POST"}`), "201 Created", false)
		}},
		{"tool error", "", func(c context.Context) { Tool(c, "telegram_send", nil, "error: offline", false) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var log Log
			ctx := With(context.Background(), work, &log)
			tc.run(ctx)
			items := log.Items()
			if tc.kind == "" {
				if len(items) != 0 {
					t.Fatalf("ordinary/uncertain call reported: %+v", items)
				}
				return
			}
			if len(items) != 1 || items[0].Kind != tc.kind {
				t.Fatalf("got %+v, want %s", items, tc.kind)
			}
			tc.run(ctx)
			if len(log.Items()) != 1 {
				t.Fatal("same kind duplicated")
			}
		})
	}
	// An explicit venv interpreter remains local even if VIRTUAL_ENV is absent.
	if err := os.WriteFile(filepath.Join(outside, "pyvenv.cfg"), []byte("home=/python"), 0600); err != nil {
		t.Fatal(err)
	}
	var log Log
	Shell(With(context.Background(), work, &log), pip+" install demo", work, nil)
	if len(log.Items()) != 0 {
		t.Fatal("venv pip counted as global")
	}
}
