//go:build smoke

package smoke

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only the provider is scripted: this calls the actual shipped binary through
// a source Shell. This sealed baseline predates the ca6a8530 contract; first
// candidates use WriteReadEntry below without repeating this accepted red.
func TestCLISkillRuntimeReadEntry(t *testing.T) {
	for _, key := range []string{"XBOT_VAULT_SOCKET", "XBOT_SMOKE_OAUTH", "XBOT_DEV_STATIC", "XBOT_ROOT", "XBOT_ENDPOINT", "XBOT_BIN", "XBOT_BOT", "XBOT_RUNNER_URL", "XBOT_RUNNER_TOKEN", "XBOT_CODE_AGENT"} {
		t.Setenv(key, "")
	}
	model := newMCPRuntimeModel(t)
	const cue = "CLI_SKILL_RUNTIME_READ_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Read a skill through the bound runtime CLI", "command": `"$XBOT_BIN" skill read runtime-entry --self --json`}))
	srv := Start(t, &Stub{srv: model.server})
	path := filepath.Join(srv.Root, "skills", "runtime-entry", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: runtime-entry\ndescription: Runtime skill entry fixture\n---\n\nRUNTIME_READ_ENTRY_BODY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.CreateBot("SkillWorker", "Independent runtime skill read acceptance.")
	cliSkillSend(t, srv, "SkillWorker", cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("source skill read did not succeed: %.1024q", results)
	}
	raw := []byte(commandOutput(results[0]))
	if len(raw) > 64<<10 || !json.Valid(raw) {
		t.Fatalf("runtime skill read did not return bounded JSON: %.1024q", raw)
	}
}

// The first product run is only this chain. It proves both typed source
// operations with title/full body and the existing omitted-scope all-local
// semantics, independently of the later budget/readonly/schema cases.
func TestCLISkillRuntimeWriteReadEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "CLI_SKILL_RUNTIME_WRITE_READ_ENTRY"
	const slug, title, description = "runtime-write-entry", "Runtime entry 标题", "Independent runtime skill fixture"
	const body = "# Runtime entry\n\nPreserve this complete body.\n\nRUNTIME_WRITE_READ_TAIL"
	write := "printf '%s' " + shellQuote(body) + ` > runtime-entry-body.md
"$XBOT_BIN" skill write ` + slug + " --title " + shellQuote(title) + " --description " + shellQuote(description) + ` --body-file runtime-entry-body.md --self --json`
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Write a complete skill through the bound runtime source", "command": write}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the written skill through the original source", "command": `"$XBOT_BIN" skill read ` + slug + ` --self --json`}),
	)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("SkillWorker", "Independent runtime skill write/read acceptance.")
	srv.CreateBot("SkillPeer", "Omitted write scope still includes the other local Bot.")
	cliSkillSend(t, srv, "SkillWorker", cue)
	results := model.results(cue)
	if len(results) != 2 {
		t.Fatalf("runtime write/read results=%d", len(results))
	}
	var written cliSkillWriteReply
	cliSkillDecode(t, results[0], &written)
	if written.Slug != slug || !written.Written || written.Error != "" {
		t.Fatalf("write receipt did not report the real file: %+v", written)
	}
	for _, name := range []string{"SkillWorker", "SkillPeer"} {
		found := false
		for _, updated := range written.UpdatedBots {
			found = found || updated == name
		}
		if !found {
			t.Fatalf("omitted scope lost local Bot %s: %+v", name, written)
		}
		cfg, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Disabled []string `json:"disabled_skills"`
		}
		if err := json.Unmarshal(cfg, &config); err != nil {
			t.Fatal(err)
		}
		for _, disabled := range config.Disabled {
			if disabled == slug {
				t.Fatalf("omitted scope disabled %s on %s", slug, name)
			}
		}
	}
	var read cliSkillReadReply
	cliSkillDecode(t, results[1], &read)
	if read.Slug != slug || read.Name != slug || read.Title != title || read.Description != description || read.Body == nil || *read.Body != body || read.File != "" || read.Error != "" {
		t.Fatalf("read did not preserve metadata and complete inline body: slug=%q name=%q title=%q description=%q bodyPresent=%t file=%q error=%q", read.Slug, read.Name, read.Title, read.Description, read.Body != nil, read.File, read.Error)
	}
	raw, err := os.ReadFile(filepath.Join(srv.Root, "skills", slug, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `title: "`+title+`"`) || !strings.HasSuffix(string(raw), body+"\n") {
		t.Fatal("actual SKILL.md lost title/full body")
	}
}

// Do not attach shared serve stderr (which contains the disposable login token)
// to failures. This keeps the existing authenticated real messages/SSE path.
func cliSkillSend(t *testing.T, srv *Server, name, cue string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"text": cue})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/bots/"+url.PathEscape(name)+"/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := srv.client.Do(req)
	if err != nil {
		t.Fatalf("private skill message: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("private skill message: HTTP %d", res.StatusCode)
	}
	scan := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	scan.Buffer(make([]byte, 4096), 1<<20)
	var event string
	done := false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			if event == "error" {
				t.Fatal("private skill message returned a stream error")
			}
			if event == "done" {
				done = true
			}
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("private skill message SSE: %v", err)
	}
	if !done {
		t.Fatal("private skill message SSE has no done event")
	}
}
