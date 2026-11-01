package kernel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/mcp"
	"xbot/internal/work"
)

func newMCPDomainResources(t *testing.T) (*BotResources, *mcp.Manager) {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "original", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := mcp.NewManager(ctx, mcp.Options{})
	r, err := NewBotResources(ctx, b, manager)
	if err != nil {
		cancel()
		_ = manager.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); cancel(); _ = manager.Close() })
	return r, manager
}

func readMCPArtifact(t *testing.T, r *BotResources, artifact *cliops.MCPArtifact) []byte {
	t.Helper()
	if artifact == nil || artifact.Access != "local" {
		t.Fatalf("missing local artifact: %+v", artifact)
	}
	r.route.RLock()
	file := filepath.Join(work.BotDir(r.current.Root, r.current.Name()), artifact.File)
	r.route.RUnlock()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMCP2bProjectionPreservesAllResultKinds(t *testing.T) {
	r, _ := newMCPDomainResources(t)
	r.Redact = func(s string) string { return strings.ReplaceAll(s, "fixture-secret", "[redacted]") }
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	image64 := base64.StdEncoding.EncodeToString(img.Bytes())
	large := strings.Repeat("large text ", 600)
	payload := map[string]any{"isError": true, "content": []any{
		map[string]any{"type": "text", "text": "small fixture-secret"},
		map[string]any{"type": "text", "text": large},
		map[string]any{"type": "image", "mimeType": "image/png", "data": image64},
		map[string]any{"type": "resource_link", "uri": "https://example.invalid/no-fetch", "extra": "kept in raw"},
		map[string]any{"type": "resource", "resource": map[string]any{"uri": "fixture:text", "mimeType": "text/plain", "text": "embedded fixture-secret"}},
		map[string]any{"type": "resource", "resource": map[string]any{"uri": "fixture:blob", "mimeType": "application/custom", "blob": base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})}},
		map[string]any{"type": "future_block", "unknown": map[string]any{"value": json.Number("9007199254740993")}},
	}, "structuredContent": map[string]any{"number": json.Number("9007199254740993"), "text": "fixture-secret"}, "extension": true}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := r.projectMCPCall(raw)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	if !reply.IsError || len(reply.ProjectionErrors) != 0 || len(reply.Content) != 7 {
		t.Fatalf("projection: %s", stdout)
	}
	if len(stdout) > cliops.MCPMaxReply || bytes.Contains(stdout, []byte(image64)) || bytes.Contains(stdout, []byte("fixture-secret")) {
		t.Fatal("unbounded or unredacted stdout")
	}
	stored := readMCPArtifact(t, r, reply.Raw)
	if bytes.Contains(stored, []byte("fixture-secret")) || !bytes.Contains(stored, []byte("9007199254740993")) || !bytes.Contains(stored, []byte(image64)) || !bytes.Contains(stored, []byte(`"extension":true`)) {
		t.Fatalf("raw lost data or secret redaction: %s", stored)
	}
	if !bytes.Contains(readMCPArtifact(t, r, reply.Structured), []byte("9007199254740993")) {
		t.Fatal("structured integer rounded")
	}
	if reply.Content[0].Text != "small [redacted]" || reply.Content[1].Text != "" || len(reply.Content[1].Preview) > mcpPreviewBytes {
		t.Fatal("text projection mismatch")
	}
	readContent := func(i int) []byte {
		return readMCPArtifact(t, r, &cliops.MCPArtifact{File: reply.Content[i].File, Access: reply.Content[i].Access})
	}
	if string(readContent(1)) != large || !bytes.Equal(readContent(2), img.Bytes()) || reply.Content[3].URI != "https://example.invalid/no-fetch" || reply.Content[3].File != "" || string(readContent(4)) != "embedded [redacted]" || !bytes.Equal(readContent(5), []byte{0, 1, 2, 3}) || !bytes.Contains(readContent(6), []byte("9007199254740993")) {
		t.Fatal("artifact content lost")
	}
}

func TestMCP2bProjectionBoundsMetadataAndManifest(t *testing.T) {
	r, _ := newMCPDomainResources(t)
	tool := json.RawMessage(`{"name":"docs/search","inputSchema":{"description":"` + strings.Repeat("<quoted>\\\"", 10000) + `"},"unknown":true}`)
	reply, err := r.mcpMetadata(tool)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Artifact == nil || len(reply.Data) != 0 {
		t.Fatalf("large metadata was inlined: %+v", reply)
	}
	stored := readMCPArtifact(t, r, reply.Artifact)
	if !bytes.Contains(stored, []byte(`"unknown":true`)) || !bytes.Contains(stored, []byte("docs/search")) {
		t.Fatal("schema/unknown metadata lost")
	}
	content := make([]any, 2000)
	for i := range content {
		content[i] = map[string]any{"type": "text", "text": strings.Repeat("<", 64)}
	}
	raw, _ := json.Marshal(map[string]any{"content": content})
	reply, err = r.projectMCPCall(raw)
	if err != nil {
		t.Fatal(err)
	}
	stdout, _ := json.Marshal(reply)
	if len(stdout) > cliops.MCPMaxReply || reply.Artifact == nil || reply.Raw == nil {
		t.Fatal("manifest budget failed")
	}
	var manifest cliops.MCPReply
	if err := json.Unmarshal(readMCPArtifact(t, r, reply.Artifact), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Content) != len(content) {
		t.Fatal("manifest truncated content metadata")
	}
}

func TestMCP2bMalformedImageAndClosedWriteReportProjectionErrors(t *testing.T) {
	r, _ := newMCPDomainResources(t)
	reply, err := r.projectMCPCall(json.RawMessage(`{"content":[{"type":"image","mimeType":"image/png","data":"not base64"}]}`))
	if err != nil || len(reply.ProjectionErrors) == 0 || reply.Content[0].File == "" {
		t.Fatalf("malformed image vanished: %+v %v", reply, err)
	}
	block := readMCPArtifact(t, r, &cliops.MCPArtifact{File: reply.Content[0].File, Access: reply.Content[0].Access})
	if !bytes.Contains(block, []byte("not base64")) {
		t.Fatal("malformed original block was lost")
	}
	r.Close()
	workspace := work.BotDir(r.current.Root, r.current.Name())
	before, err := os.ReadDir(filepath.Join(workspace, work.ResultDir))
	if err != nil {
		t.Fatal(err)
	}
	reply, err = r.projectMCPCall(json.RawMessage(`{"content":[{"type":"text","text":"after close"}]}`))
	if err != nil || len(reply.ProjectionErrors) == 0 {
		t.Fatal("closed write did not report failure")
	}
	after, err := os.ReadDir(filepath.Join(workspace, work.ResultDir))
	if err != nil || len(after) != len(before) {
		t.Fatal("closed resource wrote a new artifact")
	}
}

func TestMCP2bDomainConfigOverrideAndRenameDuringCall(t *testing.T) {
	r, manager := newMCPDomainResources(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var arguments json.RawMessage
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&frame); err != nil {
			t.Error(err)
			return
		}
		var result any = map[string]any{}
		switch frame.Method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = json.RawMessage(`{"tools":[{"name":"docs/search","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"future":true},"extension":7}]}`)
		case "tools/call":
			var params struct {
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(frame.Params, &params)
			arguments = params.Arguments
			close(started)
			select {
			case <-release:
			case <-req.Context().Done():
				return
			}
			result = json.RawMessage(`{"content":[{"type":"text","text":"finished after rename"}]}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
	}))
	t.Cleanup(func() { once.Do(func() { close(release) }); remote.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := mcp.Spec{Name: "docs", URL: remote.URL}
	if reply, err := r.MCPAdd(ctx, "", spec); err != nil || !reply.Saved || reply.RuntimeError != "" {
		t.Fatalf("global save: %+v %v", reply, err)
	}
	other, err := bot.Create(r.current.Root, "other", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherR, err := NewBotResources(r.ctx, other, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer otherR.Close()
	spec.URL = remote.URL + "/override"
	if reply, err := r.MCPAdd(ctx, "bot", spec); err != nil || !reply.Saved {
		t.Fatalf("override: %+v %v", reply, err)
	}
	list, err := r.MCPList(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Scope   string `json:"scope"`
		Servers []struct {
			Name     string     `json:"name"`
			Shadowed bool       `json:"shadowed"`
			Runtime  mcp.Status `json:"runtime"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(list.Data, &view); err != nil {
		t.Fatal(err)
	}
	if view.Scope != "global" || len(view.Servers) != 1 || !view.Servers[0].Shadowed || view.Servers[0].Runtime.State != "configured" {
		t.Fatalf("global shadowed state: %s", list.Data)
	}
	if otherR.MCP.Specs()[0].URL == spec.URL {
		t.Fatal("bot override leaked to sibling")
	}
	desc, err := r.MCPDescribe(ctx, "docs", "docs/search")
	if err != nil || !bytes.Contains(desc.Data, []byte(`"extension":7`)) || !bytes.Contains(desc.Data, []byte(`"future":true`)) {
		t.Fatalf("complete describe: %s %v", desc.Data, err)
	}
	returned := make(chan cliops.MCPReply, 1)
	failed := make(chan error, 1)
	go func() {
		reply, err := r.MCPCall(ctx, cliops.MCPCall{Server: "docs", Tool: "docs/search", Args: json.RawMessage(`{"big":9007199254740993}`), ReadOnly: true})
		returned <- reply
		failed <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("call never reached remote")
	}
	if err := r.RenameTo("renamed"); err != nil {
		t.Fatal("network held route during rename", err)
	}
	once.Do(func() { close(release) })
	var reply cliops.MCPReply
	select {
	case reply = <-returned:
	case <-ctx.Done():
		t.Fatal("call stuck")
	}
	if err := <-failed; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(arguments, []byte("9007199254740993")) {
		t.Fatal("arguments integer rounded")
	}
	if len(reply.ProjectionErrors) != 0 || !bytes.Contains(readMCPArtifact(t, r, reply.Raw), []byte("finished after rename")) {
		t.Fatalf("late result projection: %+v", reply)
	}
	if _, err := os.Stat(work.BotDir(r.current.Root, "original")); !os.IsNotExist(err) {
		t.Fatalf("old workspace recreated: %v", err)
	}
	if err := os.WriteFile(r.current.MCPPath(), []byte("broken config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.MCP.Reconcile(ctx); err == nil {
		t.Fatal("bad config not reported")
	}
	if _, err := r.MCPList(ctx, "bot"); err == nil {
		t.Fatal("explicit config list hid malformed file")
	}
	if _, err := r.MCPTools(ctx, "docs"); err != nil {
		t.Fatal("bad config discarded retained runtime", err)
	}
	if reply, err := otherR.MCPAdd(ctx, "global", mcp.Spec{Name: "more", URL: remote.URL}); err != nil || !reply.Saved || reply.RuntimeError == "" {
		t.Fatalf("saved/reconcile failure conflated: %+v %v", reply, err)
	}
}
