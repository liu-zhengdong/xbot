//go:build smoke

package smoke

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These DTOs observe the fixed public contract, not internal product helpers.
type cliFullProfile struct {
	Name          string   `json:"name"`
	PreviousName  string   `json:"previous_name"`
	Description   string   `json:"description"`
	Timezone      string   `json:"timezone"`
	Avatar        string   `json:"avatar"`
	AvatarSeed    string   `json:"avatar_seed"`
	Scope         string   `json:"scope"`
	Applied       bool     `json:"applied"`
	AppliedFields []string `json:"applied_fields"`
	Moved         bool     `json:"moved"`
	Error         string   `json:"error"`
}

func cliFullValue(t *testing.T, raw []byte) cliFullProfile {
	t.Helper()
	if len(raw) > 64<<10 {
		t.Fatalf("profile metadata exceeded 64KiB: %d bytes", len(raw))
	}
	var value cliFullProfile
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("profile output is not one JSON object: %v", err)
	}
	return value
}
func cliFullShell(t *testing.T, result string) cliFullProfile {
	t.Helper()
	if !strings.HasPrefix(result, "exit 0\n") {
		t.Fatalf("real source profile CLI failed: %s", result)
	}
	value := cliFullValue(t, []byte(commandOutput(result)))
	if value.Scope != "runtime" {
		t.Fatalf("profile used the wrong scope: %+v", value)
	}
	return value
}

// Keep failure diagnostics private: shared harness transport errors include
// startup stderr. New requests report only method/path/status or their error.
func cliFullAPI(t *testing.T, srv *Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 1<<20 {
		t.Fatal("API response exceeded 1MiB")
	}
	return res.StatusCode, raw
}

type cliFullNotice struct {
	Name         string `json:"name"`
	PreviousName string `json:"previous_name"`
}

func cliFullSend(t *testing.T, srv *Server, name, cue string) []cliFullNotice {
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
		t.Fatalf("private message %s: %v", cue, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("private message %s: HTTP %d", cue, res.StatusCode)
	}
	scan := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	scan.Buffer(make([]byte, 4096), 1<<20)
	var event string
	var notices []cliFullNotice
	done := false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch event {
			case "error":
				t.Fatalf("private message %s returned a stream error", cue)
			case "done":
				done = true
			case "profile":
				if len(data) > 64<<10 {
					t.Fatal("profile SSE exceeded metadata budget")
				}
				var notice cliFullNotice
				if err := json.Unmarshal([]byte(data), &notice); err != nil {
					t.Fatal(err)
				}
				notices = append(notices, notice)
			}
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("private message %s: %v", cue, err)
	}
	if !done {
		t.Fatalf("private message %s ended without done", cue)
	}
	return notices
}

type cliFullModel struct {
	base        *mcpRuntimeModel
	server      *httptest.Server
	mu          sync.Mutex
	oldProfile  bool
	childModels []string
}

func newCLIProfileFullModel(t *testing.T, child func(http.ResponseWriter, *http.Request, continuationRequest)) *cliFullModel {
	t.Helper()
	m := &cliFullModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad model input", 400)
			return
		}
		var req continuationRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "bad model JSON", 400)
			return
		}
		caller, _ := classify(req.stub())
		if caller != callerCompaction && caller != callerExtract {
			m.mu.Lock()
			m.oldProfile = m.oldProfile || bytes.Contains(raw, []byte("bot_update_profile"))
			if caller == callerSubagent {
				m.childModels = append(m.childModels, req.Model)
			}
			m.mu.Unlock()
		}
		if caller == callerSubagent && child != nil {
			child(w, r, req)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
func (m *cliFullModel) assertRetired(t *testing.T) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.oldProfile {
		t.Fatal("actual provider request still exposes the retired profile tool")
	}
}

func cliFullAbsent(t *testing.T, root, name string) {
	t.Helper()
	for _, path := range []string{filepath.Join(root, "bots", name), filepath.Join(root, "work", name)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("old runtime directory reappeared: %s (%v)", path, err)
		}
	}
}
func cliFullRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func cliFullSnapshot(t *testing.T, root, name string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, file := range []string{"config.json", "persona.md", "avatar", "avatar.mime"} {
		raw, err := os.ReadFile(filepath.Join(root, "bots", name, file))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out[file] = raw
	}
	return out
}
func cliFullUnchanged(t *testing.T, root, name string, before map[string][]byte) {
	t.Helper()
	after := cliFullSnapshot(t, root, name)
	for file, raw := range before {
		if !bytes.Equal(raw, after[file]) {
			t.Fatalf("predictable rejection changed %s", file)
		}
	}
}
func cliFullJob(t *testing.T, srv *Server, name, id string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(cliFullRead(t, filepath.Join(srv.Root, "bots", name, "shell-jobs", id+".json")), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func cliFullAgent(t *testing.T, srv *Server, name, id string) continuationAgent {
	t.Helper()
	status, raw := cliFullAPI(t, srv, http.MethodGet, "/api/bots/"+name, nil)
	if status != 200 {
		t.Fatalf("current Bot panel: HTTP %d", status)
	}
	var data struct{ Agents []continuationAgent }
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for _, agent := range data.Agents {
		if agent.ID == id {
			return agent
		}
	}
	t.Fatalf("original child %s missing under %s", id, name)
	return continuationAgent{}
}
func cliFullOneInbox(t *testing.T, srv *Server, name, source, kind, sender, marker string) {
	t.Helper()
	WaitFor(t, 10*time.Second, "original event in current Inbox", func() (bool, string) {
		count := 0
		for _, event := range mcpRuntimeInbox(t, srv, name) {
			if event["source"] == source && event["type"] == kind && (sender == "" || event["sender"] == sender) {
				raw, _ := json.Marshal(event)
				if strings.Contains(string(raw), marker) {
					count++
				}
			}
		}
		if count > 1 {
			t.Fatalf("duplicate original %s event", kind)
		}
		return count == 1, fmt.Sprintf("matching events=%d", count)
	})
}
