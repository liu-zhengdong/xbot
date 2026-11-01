package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/kernel"
	"xbot/internal/pipe/limits"
	"xbot/internal/provider"
)

func subagentHTTPRig(t *testing.T) (*Server, *bot.Bot, string, func(string, string, string) *httptest.ResponseRecorder) {
	t.Helper()
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(s.Root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	h := muxOf(s)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	return s, b, token, request
}

func writeSubagentFixture(t *testing.T, b *bot.Bot, id string, entries []bot.Entry, continuable bool) {
	t.Helper()
	dir := b.AgentDir(id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{"id": id, "task": "检查模块", "status": "done", "turn_seq": 7, "result_path": filepath.Join(dir, "results", "7.txt")}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if continuable {
		descriptor := map[string]any{"version": 1, "id": id, "bot": b.Name(), "type": "research", "mode": "brief", "cwd": t.TempDir(), "provider": "fake", "model": "scripted", "tools": []string{"read_file"}, "max_steps": 10, "timeout_sec": 60}
		data, err := json.Marshal(descriptor)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "descriptor.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestContinuableSubagentTranscriptHTTP(t *testing.T) {
	s, b, token, request := subagentHTTPRig(t)
	id := "sa_0123456789abcdef"
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	var entries []bot.Entry
	for n := 1; n <= 45; n++ {
		entries = append(entries, bot.Entry{Time: at, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(fmt.Sprintf("输入 %d", n))}})
	}
	entries[43].Message = provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{
		{Type: provider.PartReasoning, Text: "private reasoning", VendorRaw: json.RawMessage(`{"secret":"private replay"}`)},
		{Type: provider.PartText, Text: "fake-"}, {Type: provider.PartText, Text: "secret"},
		{Type: provider.PartToolCall, ToolCall: &provider.ToolCall{ID: "call1", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"fake-secret"}`}}},
		{Type: provider.PartHosted, Text: "公开摘要", VendorRaw: json.RawMessage(`{"secret":"private replay"}`)},
	}}
	entries[44].Message = provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("call1", "已读 fake-secret")}
	writeSubagentFixture(t, b, id, entries, true)
	// An in-progress append is not a stable transcript row.
	f, err := os.OpenFile(filepath.Join(b.AgentDir(id), "transcript.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"message":`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	restore := bot.SetRedactText(func(s string) string { return strings.ReplaceAll(s, "fake-secret", "[secret]") })
	defer restore()
	base := "/api/bots/ada/agents/" + id + "/transcript"
	res := request("GET", base, "")
	if res.Code != 200 {
		t.Fatalf("tail: %d %s", res.Code, res.Body.String())
	}
	var out subagentTranscript
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != id || len(out.Entries) != 40 || out.Entries[0].Seq != 6 || out.Entries[39].Seq != 45 || !out.HasMore || out.NextBefore != 6 || out.TurnSeq != 7 || out.Status != "done" || !out.Continuable {
		t.Fatalf("tail metadata: %+v", out)
	}
	if strings.Contains(res.Body.String(), "private") || strings.Contains(res.Body.String(), "reasoning") || strings.Contains(res.Body.String(), "vendor_raw") || strings.Contains(res.Body.String(), "fake-secret") {
		t.Fatalf("private content exposed: %s", res.Body.String())
	}
	if len(out.Entries[38].Message.Calls()) != 1 || out.Entries[38].Message.Calls()[0].Function.Name != "read_file" || out.Entries[39].Message.CallID() != "call1" || !strings.Contains(out.Entries[39].Message.Text(), "[secret]") {
		t.Fatal("tool call/result lost from public blocks")
	}
	res = request("GET", base+"?before=6&limit=40", "")
	out = subagentTranscript{}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 5 || out.Entries[0].Seq != 1 || out.Entries[4].Seq != 5 || out.HasMore || out.NextBefore != 0 {
		t.Fatalf("oldest page: %s", res.Body.String())
	}
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=bad", "?before=0", "?before=-1", "?before=bad", "?limit=1&limit=2"} {
		if got := request("GET", base+q, ""); got.Code != 400 {
			t.Errorf("%s: %d", q, got.Code)
		}
	}
	unauth := httptest.NewRecorder()
	muxOf(s).ServeHTTP(unauth, httptest.NewRequest("GET", base, nil))
	if unauth.Code != 401 {
		t.Fatalf("unauth: %d", unauth.Code)
	}
	other, err := bot.Create(s.Root, "other", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetOwners([]string{"u2"}); err != nil {
		t.Fatal(err)
	}
	writeSubagentFixture(t, other, id, entries[:1], true)
	alias := "sa_4123456789abcdef"
	writeSubagentFixture(t, other, alias, entries[:1], true)
	if err := os.Symlink(other.AgentDir(alias), b.AgentDir(alias)); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/api/bots/ada/agents/"+alias+"/transcript", ""); got.Code != 404 {
		t.Fatalf("cross-owner task symlink: %d %s", got.Code, got.Body.String())
	}
	for _, path := range []string{"/api/bots/other/agents/" + id + "/transcript", "/api/bots/ada/agents/sa_missing0000/transcript", "/api/bots/ada/agents/sa_%2Foutside/transcript"} {
		if got := request("GET", path, ""); got.Code != 404 {
			t.Errorf("isolation %s: %d %s", path, got.Code, got.Body.String())
		}
	}
	for _, suffix := range []string{"messages", "stop"} {
		ownPath := "/api/bots/ada/agents/" + id + "/" + suffix
		otherPath := "/api/bots/other/agents/" + id + "/" + suffix
		if got := request("POST", otherPath, `{}`); got.Code != 404 {
			t.Errorf("cross-owner %s: %d", suffix, got.Code)
		}
		w := httptest.NewRecorder()
		muxOf(s).ServeHTTP(w, httptest.NewRequest("POST", ownPath, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Errorf("unauthenticated %s: %d", suffix, w.Code)
		}
		r := httptest.NewRequest("POST", ownPath, strings.NewReader(`{}`))
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		r.Header.Set("Origin", "https://unrelated.example")
		w = httptest.NewRecorder()
		muxOf(s).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("cross-origin %s: %d", suffix, w.Code)
		}
	}
	// Read-only endpoints work without a Kernel factory and never construct one.
	if len(s.kernels) != 0 {
		t.Fatal("transcript created a Kernel")
	}
}

func TestContinuableSubagentTranscriptBudgetAndLegacy(t *testing.T) {
	_, b, _, request := subagentHTTPRig(t)
	id := "sa_1123456789abcdef"
	var entries []bot.Entry
	for n := 0; n < 35; n++ {
		entries = append(entries, bot.Entry{Time: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("tool", strings.Repeat("<&😀", 1400))}})
	}
	entries = append(entries, bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "huge", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: strings.Repeat("原文", 100000)}}})}})
	writeSubagentFixture(t, b, id, entries, false)
	base := "/api/bots/ada/agents/" + id + "/transcript"
	res := request("GET", base+"?limit=200", "")
	if res.Code != 200 || res.Body.Len() > limits.MaxBody {
		t.Fatalf("budget: %d %d", res.Code, res.Body.Len())
	}
	var out subagentTranscript
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Continuable || !out.HasMore || out.NextBefore < 2 || out.Entries[len(out.Entries)-1].Seq != len(entries) || !out.Entries[len(out.Entries)-1].Truncated || strings.TrimSpace(out.Entries[len(out.Entries)-1].Message.Text()) == "" {
		t.Fatalf("budget preview/cursor: %+v", out)
	}
	cursor := out.NextBefore
	res = request("GET", base+fmt.Sprintf("?before=%d&limit=200", cursor), "")
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) == 0 || out.Entries[len(out.Entries)-1].Seq != cursor-1 {
		t.Fatalf("pagination skipped row before %d", cursor)
	}
	res = request("GET", "/api/bots/ada", "")
	var panel struct {
		Agents []struct {
			ID          string
			Continuable bool
			TurnSeq     int    `json:"turn_seq"`
			ResultPath  string `json:"result_path"`
		}
	}
	if err := json.Unmarshal(res.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if len(panel.Agents) != 1 || panel.Agents[0].ID != id || panel.Agents[0].Continuable || panel.Agents[0].TurnSeq != 7 || !strings.HasSuffix(panel.Agents[0].ResultPath, "/results/7.txt") {
		t.Fatalf("legacy details: %s", res.Body.String())
	}
	// A stored meta ID cannot redirect the response or a later control.
	if err := os.WriteFile(filepath.Join(b.AgentDir(id), "meta.json"), []byte(`{"id":"sa_somewhereelse","status":"done"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", base, ""); got.Code != 404 {
		t.Fatalf("mismatched ID: %d", got.Code)
	}
	res = request("GET", "/api/bots/ada", "")
	if strings.Contains(res.Body.String(), "sa_somewhereelse") {
		t.Fatal("panel trusted stored ID")
	}
}

func TestContinuableSubagentControlJSON(t *testing.T) {
	_, _, _, request := subagentHTTPRig(t)
	for _, suffix := range []string{"messages", "stop"} {
		path := "/api/bots/ada/agents/sa_0123456789abcdef/" + suffix
		for _, body := range []string{`{"unknown":true}`, `{`, `{} {}`, "{}" + strings.Repeat(" ", 64<<10)} {
			if got := request("POST", path, body); got.Code != 400 {
				t.Errorf("%s %q: %d", suffix, body[:min(40, len(body))], got.Code)
			}
		}
	}
}

func TestContinuableSubagentTypedErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{exec.ErrNotFound, 404}, {exec.ErrNotContinuable, 409}, {exec.ErrActiveElsewhere, 409}, {exec.ErrQueueFull, 429}} {
		w := httptest.NewRecorder()
		writeSubagentError(w, fmt.Errorf("wrapped: %w", tc.err))
		if w.Code != tc.code {
			t.Errorf("%v: %d", tc.err, w.Code)
		}
	}
}

type subagentContinuationModel struct{ requests chan provider.Request }

func (m subagentContinuationModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	m.requests <- req
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第二轮结果")}, nil
}

func TestContinuableSubagentContinuesCompletedHTTP(t *testing.T) {
	s, b, _, request := subagentHTTPRig(t)
	id := "sa_5123456789abcdef"
	writeSubagentFixture(t, b, id, []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("FIRST_NECESSARY_FACT")}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("第一轮结果")}},
	}, true)
	meta := exec.Meta{ID: id, Status: "done", TurnSeq: 1}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AgentDir(id), "meta.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	requests := make(chan provider.Request, 1)
	idle := make(chan struct{})
	local := exec.NewLocal(exec.Limits{MaxConcurrent: 3, MaxSteps: 2, Timeout: 5 * time.Second})
	local.AgentsDir = b.AgentsDir()
	local.Prov = subagentContinuationModel{requests: requests}
	local.Restore = func(context.Context, exec.Descriptor) (exec.StartRequest, error) { return exec.StartRequest{}, nil }
	local.OnIdle = func() { close(idle) }
	t.Cleanup(func() {
		tasks, _ := local.Tasks()
		for _, task := range tasks {
			_ = local.Control(context.Background(), task.ID, exec.Control{Kind: exec.KindCancel})
		}
		if len(tasks) > 0 {
			select {
			case <-idle:
			case <-time.After(3 * time.Second):
				t.Error("continued task did not finish writes")
			}
		}
	})
	s.NewKernel = func(loaded *bot.Bot, _ *kernel.BotResources) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: loaded, Provider: local.Prov, Exec: local}, nil
	}
	base := "/api/bots/ada/agents/" + id
	res := request("POST", base+"/messages", `{"text":"沿原结论继续"}`)
	if res.Code != 200 {
		t.Fatalf("continue completed child: %d %s", res.Code, res.Body.String())
	}
	var receipt exec.Task
	if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != id || receipt.Status != "running" || receipt.TurnSeq <= 2 {
		t.Fatalf("continued admission: %+v", receipt)
	}
	select {
	case <-idle:
	case <-time.After(3 * time.Second):
		t.Fatal("continued task did not finish")
	}
	req := <-requests
	var contextText strings.Builder
	for _, m := range req.Messages {
		contextText.WriteString(m.Text())
	}
	if !strings.Contains(contextText.String(), "FIRST_NECESSARY_FACT") || !strings.Contains(contextText.String(), "沿原结论继续") {
		t.Fatal("HTTP feedback did not continue the original child context")
	}
	res = request("GET", base+"/transcript", "")
	var transcript subagentTranscript
	if err := json.Unmarshal(res.Body.Bytes(), &transcript); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || transcript.Status != "done" || transcript.TurnSeq != receipt.TurnSeq || !transcript.Continuable {
		t.Fatalf("continued transcript: %d %s", res.Code, res.Body.String())
	}
	end := transcript.Entries[len(transcript.Entries)-1]
	if end.Kind != "turn_end" || end.TurnSeq != receipt.TurnSeq || end.Status != "done" || end.ResultPath == "" {
		t.Fatalf("terminal public projection lost identity/result: %+v", end)
	}
}

func TestContinuableSubagentLegacyControls(t *testing.T) {
	s, b, _, request := subagentHTTPRig(t)
	s.NewKernel = func(loaded *bot.Bot, _ *kernel.BotResources) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: loaded, Exec: &bindingTasks{}}, nil
	}
	_, gate, err := s.kernel(b)
	if err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	defer gate.Unlock()
	for _, action := range []string{"steer", "queue", "interrupt", "cancel"} {
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- request("POST", "/api/bots/ada/agents/sa_legacy0001/"+action, `{"text":"补充说明"}`)
		}()
		select {
		case res := <-result:
			if res.Code != 200 || !strings.Contains(res.Body.String(), action+" sa_legacy0001") {
				t.Fatalf("legacy %s: %d %s", action, res.Code, res.Body.String())
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("legacy %s waited on foreground gate", action)
		}
	}
	if got := request("POST", "/api/bots/ada/agents/sa_legacy0001/messages", `{"text":"继续"}`); got.Code != 409 {
		t.Fatalf("old executor was treated as continuable: %d %s", got.Code, got.Body.String())
	}
}

type subagentHoldingModel struct{ calls chan struct{} }

func (m subagentHoldingModel) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	m.calls <- struct{}{}
	<-ctx.Done()
	return provider.Message{}, ctx.Err()
}

func TestContinuableSubagentControlsReachActiveOwnerWithoutGate(t *testing.T) {
	s, b, _, request := subagentHTTPRig(t)
	id := "sa_2123456789abcdef"
	calls := make(chan struct{}, 4)
	idle := make(chan struct{}, 1)
	owner := exec.NewLocal(exec.Limits{MaxConcurrent: 1, MaxSteps: 10, Timeout: time.Minute})
	owner.AgentsDir = b.AgentsDir()
	owner.Prov = subagentHoldingModel{calls: calls}
	owner.OnIdle = func() { idle <- struct{}{} }
	descriptor := &exec.Descriptor{Version: 1, ID: id, Bot: b.Name(), Type: "research", Mode: exec.ModeBrief,
		CWD: t.TempDir(), Provider: "fake", Model: "scripted", Tools: []string{}, MaxSteps: 10, TimeoutSec: 60}
	original, err := owner.Start(context.Background(), exec.StartRequest{ID: id, Bot: b.Name(), Task: "hold until stopped", Mode: exec.ModeBrief,
		Type: "research", Dir: b.AgentDir(id), Descriptor: descriptor})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-calls:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not start")
	}
	t.Cleanup(func() {
		tasks, _ := owner.Tasks()
		for _, task := range tasks {
			_ = owner.Control(context.Background(), task.ID, exec.Control{Kind: exec.KindCancel})
		}
		select {
		case <-idle:
		case <-time.After(3 * time.Second):
			t.Error("owner did not finish writes")
		}
	})
	other := exec.NewLocal(exec.Limits{MaxConcurrent: 3})
	other.AgentsDir = b.AgentsDir()
	other.Prov = subagentHoldingModel{calls: calls}
	s.NewKernel = func(loaded *bot.Bot, _ *kernel.BotResources) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: loaded, Exec: other, Provider: other.Prov}, nil
	}
	k, gate, err := s.kernel(b)
	if err != nil {
		t.Fatal(err)
	}
	originalBot := k.Bot
	gate.Lock()
	defer gate.Unlock()
	bounded := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { result <- request(method, path, body) }()
		select {
		case res := <-result:
			return res
		case <-time.After(3 * time.Second):
			t.Fatalf("child endpoint waited on main gate: %s", path)
			return nil
		}
	}
	queuedID := "sa_3123456789abcdef"
	queuedDescriptor := *descriptor
	queuedDescriptor.ID = queuedID
	queued, err := owner.Start(context.Background(), exec.StartRequest{ID: queuedID, Bot: b.Name(), Task: "waiting", Mode: exec.ModeBrief,
		Type: "research", Dir: b.AgentDir(queuedID), Descriptor: &queuedDescriptor})
	if err != nil || queued.Status != "queued" || queued.Position != 1 {
		t.Fatalf("queue setup: %+v %v", queued, err)
	}
	queuedBase := "/api/bots/ada/agents/" + queuedID
	queuedMessage := bounded("POST", queuedBase+"/messages", `{"text":"排队时补充"}`)
	var queuedReceipt exec.Task
	if err := json.Unmarshal(queuedMessage.Body.Bytes(), &queuedReceipt); err != nil {
		t.Fatal(err)
	}
	if queuedMessage.Code != 200 || queuedReceipt.Status != "queued" || queuedReceipt.Position != 1 || queuedReceipt.TurnSeq != queued.TurnSeq {
		t.Fatalf("queued message receipt: %d %s", queuedMessage.Code, queuedMessage.Body.String())
	}
	for n := 0; n < 2; n++ {
		res := bounded("POST", queuedBase+"/stop", `{}`)
		var receipt exec.Task
		if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		if res.Code != 200 || receipt.ID != queuedID || receipt.Status != "cancelled" {
			t.Fatalf("queued/idempotent stop: %d %s", res.Code, res.Body.String())
		}
	}
	base := "/api/bots/ada/agents/" + id
	staleMeta, err := exec.ReadMeta(b.AgentDir(id))
	if err != nil {
		t.Fatal(err)
	}
	staleMeta.Status, staleMeta.TurnSeq = "done", original.TurnSeq+100
	staleMeta.FinishedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	staleRaw, err := json.Marshal(staleMeta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AgentDir(id), "meta.json"), staleRaw, 0600); err != nil {
		t.Fatal(err)
	}
	liveRead := bounded("GET", base+"/transcript", "")
	var live subagentTranscript
	if err := json.Unmarshal(liveRead.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if liveRead.Code != 200 || live.Status != "running" || live.TurnSeq != original.TurnSeq {
		t.Fatalf("disk status overrode admission: %d %s", liveRead.Code, liveRead.Body.String())
	}
	panelRead := bounded("GET", "/api/bots/ada", "")
	var panel struct {
		Agents []struct {
			ID         string     `json:"id"`
			Status     string     `json:"status"`
			FinishedAt *time.Time `json:"finished_at"`
		}
	}
	if err := json.Unmarshal(panelRead.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	foundLive := false
	for _, agent := range panel.Agents {
		if agent.ID == id {
			foundLive = agent.Status == "running" && agent.FinishedAt == nil
		}
	}
	if !foundLive {
		t.Fatalf("panel reported old terminal state for active child: %s", panelRead.Body.String())
	}
	for _, body := range []string{`{"text":" "}`, `{"text":"hello","delivery":"interrupt"}`, `{"text":"hello","sender":"bot:other"}`, `{"text":"` + strings.Repeat("x", 16385) + `"}`} {
		if got := bounded("POST", base+"/messages", body); got.Code != 400 {
			t.Fatalf("invalid message: %d %s", got.Code, got.Body.String())
		}
	}
	res := bounded("POST", base+"/messages", `{"text":"继续检查"}`)
	if res.Code != 200 {
		t.Fatalf("message: %d %s", res.Code, res.Body.String())
	}
	var receipt exec.Task
	if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != id || receipt.Status != "running" || receipt.TurnSeq != original.TurnSeq {
		t.Fatalf("receipt: %+v", receipt)
	}
	res = bounded("GET", base+"/transcript", "")
	var transcript subagentTranscript
	if err := json.Unmarshal(res.Body.Bytes(), &transcript); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || transcript.Status != "running" || transcript.TurnSeq != original.TurnSeq || !transcript.Continuable {
		t.Fatalf("live read: %d %s", res.Code, res.Body.String())
	}
	foundOwner := false
	for _, row := range transcript.Entries {
		foundOwner = foundOwner || row.Sender == "owner"
	}
	if !foundOwner {
		t.Fatal("accepted direct message lost owner source")
	}
	res = bounded("POST", base+"/stop", `{}`)
	if res.Code != 200 {
		t.Fatalf("stop: %d %s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != id || receipt.Status != "stopping" && receipt.Status != "cancelled" {
		t.Fatalf("stop receipt: %+v", receipt)
	}
	if k.Bot != originalBot {
		t.Fatal("child control replaced the active Kernel Bot")
	}
	tasks, err := other.Tasks()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("HTTP created duplicate task: %+v %v", tasks, err)
	}
	select {
	case <-calls:
		t.Fatal("HTTP started a second model call")
	default:
	}
}
