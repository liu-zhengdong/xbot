package cliops

import (
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
)

func TestMemoryRecallRuntimeTransportCanonicalWireAndRequiredCallID(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, err := io.ReadAll(r.Body)
		if err != nil || r.Method != "POST" || r.URL.Path != "/v1/memory/recall" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("X-XBot-Call-ID") != "actual-original-call" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request path=%s root=%q call=%q err=%v", r.URL.Path, r.Header.Get("X-XBot-Root"), r.Header.Get("X-XBot-Call-ID"), err)
		}
		if string(raw) != `{"query":"\u003c\u0026\u003e\n","scope":" BOT ","since":" 2026-11-02 "}` {
			t.Errorf("wire=%s", raw)
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(MemoryRecallReply{Text: "original text", Error: "source scope rejection"})
	}))
	defer source.Close()
	input := MemoryRecall{Query: "<&>\n", Scope: " BOT ", Since: " 2026-11-02 "}
	if _, err := CallMemoryRecall(context.Background(), source.URL, alias, "", input); err == nil || calls != 0 {
		t.Fatal("missing source ID sent")
	}
	reply, err := CallMemoryRecall(context.Background(), source.URL, alias, "actual-original-call", input)
	if err == nil || reply.Text != "original text" || reply.Error != "source scope rejection" || calls != 1 {
		t.Fatalf("reply=%+v calls=%d err=%v", reply, calls, err)
	}
}

func TestMemoryRecallRuntimeTransportStrictWireAndRawScope(t *testing.T) {
	calls := 0
	var got MemoryRecall
	callback := func(ctx context.Context, input MemoryRecall) (MemoryRecallReply, error) {
		calls++
		got = input
		if SourceCallID(ctx) != "actual-call" {
			t.Errorf("call ID=%q", SourceCallID(ctx))
		}
		return MemoryRecallReply{Text: "no matches"}, nil
	}
	for _, raw := range []string{"", "null", "[]", `"body"`, `true`, `{} {}`, `{} true`, `{} garbage`, `{"self":true}`, `{"query_file":"fake"}`, `{"call_id":"fake"}`, `{"limit":20}`, `{"query":[]}`, `{"scope":true}`, `{"since":1}`, strings.Repeat(" ", MemoryRecallMaxRequest+1), `{"query":"` + strings.Repeat("<", MemoryRecallMaxRequest/6+1) + `"}`} {
		w := httptest.NewRecorder()
		serveMemoryRecall(w, httptest.NewRequest("POST", "/v1/memory/recall", strings.NewReader(raw)), callback)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("bad bytes=%d code=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "/v1/memory/recall", nil),
		httptest.NewRequest("PUT", "/v1/memory/recall", strings.NewReader(`{}`)),
		httptest.NewRequest("POST", "/v1/memory/recall?query=x", strings.NewReader(`{}`)),
	} {
		w := httptest.NewRecorder()
		serveMemoryRecall(w, request, callback)
		if w.Code < 400 || calls != 0 {
			t.Fatal("method/query accepted")
		}
	}
	for _, tc := range []struct {
		raw  string
		want MemoryRecall
	}{
		{`{}`, MemoryRecall{}},
		{`{"query":"","scope":" BOT ","since":"invalid date"}`, MemoryRecall{Scope: " BOT ", Since: "invalid date"}},
		{`{"query":"` + strings.Repeat("q", 7000) + `"}`, MemoryRecall{Query: strings.Repeat("q", 7000)}},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/memory/recall", strings.NewReader(tc.raw))
		r.Header.Set("X-XBot-Call-ID", "actual-call")
		serveMemoryRecall(w, r, callback)
		if w.Code != http.StatusOK || got != tc.want {
			t.Fatalf("wire imposed domain rules code=%d got=%+v", w.Code, got)
		}
	}
	base := `{"query":""}`
	exact := `{"query":"` + strings.Repeat("x", MemoryRecallMaxRequest-len(base)) + `"}`
	for _, raw := range []string{exact, exact + " "} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/memory/recall", strings.NewReader(raw))
		r.Header.Set("X-XBot-Call-ID", "actual-call")
		serveMemoryRecall(w, r, callback)
		want := http.StatusOK
		if len(raw) > MemoryRecallMaxRequest {
			want = http.StatusBadRequest
		}
		if w.Code != want {
			t.Fatalf("raw bytes=%d code=%d want=%d", len(raw), w.Code, want)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d want=4", calls)
	}
}

func TestMemoryRecallRuntimeTransportMountedGuardsAndSourceContext(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := Start(root, Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	reply, callErr := CallMemoryRecall(context.Background(), missing.Endpoint, root, "actual-call", MemoryRecall{})
	missing.Close()
	if callErr == nil || reply.Error == "" {
		t.Fatal("missing callback succeeded")
	}
	calls := 0
	source, err := Start(root, Handlers{Recall: func(ctx context.Context, input MemoryRecall) (MemoryRecallReply, error) {
		calls++
		if SourceCallID(ctx) != "actual-call" {
			return MemoryRecallReply{}, errors.New("source requires actual prepared call")
		}
		return MemoryRecallReply{Text: "no matches"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		root, host, header, value string
		status                    int
	}{
		{root: "wrong", status: 409}, {root: canonical, host: "localhost:1", status: 403},
		{root: canonical, header: "Origin", value: "http://127.0.0.1", status: 403},
		{root: canonical, header: "Sec-Fetch-Site", value: "same-origin", status: 403},
	} {
		r, err := http.NewRequest("POST", source.Endpoint+"/v1/memory/recall", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-XBot-Root", tc.root)
		r.Header.Set("X-XBot-Call-ID", "actual-call")
		if tc.host != "" {
			r.Host = tc.host
		}
		if tc.header != "" {
			r.Header.Set(tc.header, tc.value)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status || calls != 0 {
			t.Fatalf("guard=%+v code=%d calls=%d", tc, response.StatusCode, calls)
		}
	}
	// The mounted handler transports the header; the source callback owns live
	// execution validation, including missing and stale IDs.
	for _, id := range []string{"", "stale-call", "actual-call"} {
		r, _ := http.NewRequest("POST", source.Endpoint+"/v1/memory/recall", strings.NewReader(`{}`))
		r.Header.Set("X-XBot-Root", canonical)
		r.Header.Set("X-XBot-Call-ID", id)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusBadRequest
		if id == "actual-call" {
			want = http.StatusOK
		}
		if response.StatusCode != want {
			t.Fatalf("ID=%q status=%d", id, response.StatusCode)
		}
	}
	for _, endpoint := range []string{"", "https://127.0.0.1:1", "http://localhost:1", "http://192.0.2.1:1", source.Endpoint + "/path", source.Endpoint + "?q=x", source.Endpoint + "#fragment", "http://user@127.0.0.1:1"} {
		if _, err := CallMemoryRecall(context.Background(), endpoint, root, "actual-call", MemoryRecall{}); err == nil {
			t.Errorf("invalid endpoint accepted %q", endpoint)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d want=3", calls)
	}
}

func TestMemoryRecallRuntimeTransportBudgetsPartialFactsAndNoRetry(t *testing.T) {
	root := t.TempDir()
	calls := 0
	body := `{"file":"/current/results/recall.json","access":"local","bytes":36712}`
	status := http.StatusServiceUnavailable
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer source.Close()
	for _, query := range []string{strings.Repeat("x", MemoryRecallMaxRequest+1), strings.Repeat("<", MemoryRecallMaxRequest/6+1)} {
		if _, err := CallMemoryRecall(context.Background(), source.URL, root, "actual-call", MemoryRecall{Query: query}); err == nil || calls != 0 {
			t.Fatal("oversized request sent")
		}
	}
	reply, err := CallMemoryRecall(context.Background(), source.URL, root, "actual-call", MemoryRecall{})
	if err == nil || reply.File != "/current/results/recall.json" || reply.Access != "local" || reply.Bytes != 36712 || reply.Error == "" {
		t.Fatalf("partial=%+v err=%v", reply, err)
	}
	base := `{"file":"/current/results/recall.json","access":"local","bytes":36712,"error":""}`
	for _, size := range []int{MemoryRecallMaxReply, MemoryRecallMaxReply + 1} {
		body = strings.Replace(base, `"error":""`, `"error":"`+strings.Repeat("x", size-len(base))+`"`, 1)
		got, err := CallMemoryRecall(context.Background(), source.URL, root, "actual-call", MemoryRecall{})
		if err == nil {
			t.Fatal("error/oversized reply succeeded")
		}
		if size == MemoryRecallMaxReply && (got.File != reply.File || got.Access != reply.Access || got.Bytes != reply.Bytes || got.Error == "") {
			t.Fatalf("exact HTTP budget lost facts %+v", got)
		}
		if size > MemoryRecallMaxReply && got != (MemoryRecallReply{}) {
			t.Fatalf("HTTP max+1 decoded %+v", got)
		}
	}
	for _, raw := range []string{"not JSON", `{} {}`} {
		body = raw
		if _, err := CallMemoryRecall(context.Background(), source.URL, root, "actual-call", MemoryRecall{}); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	if calls != 5 {
		t.Fatalf("calls=%d want=5 without retries", calls)
	}
	w := httptest.NewRecorder()
	serveMemoryRecall(w, httptest.NewRequest("POST", "/v1/memory/recall", strings.NewReader(`{}`)), func(context.Context, MemoryRecall) (MemoryRecallReply, error) {
		return MemoryRecallReply{File: "/current/results/recall.json", Access: "local", Bytes: 36712}, errors.New(strings.Repeat("\x00", 12<<10))
	})
	var got MemoryRecallReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.File != reply.File || got.Access != reply.Access || got.Bytes != reply.Bytes || got.Error == "" || w.Code != 400 || w.Body.Len() > memoryRecallOutputBudget || len(FormatMemoryRecall(got)) > memoryRecallOutputBudget {
		t.Fatalf("bounded error lost metadata=%+v bytes=%d err=%v", got, w.Body.Len(), err)
	}
}

func TestMemoryRecallRuntimeTransportNoRedirectOrCancelledFallback(t *testing.T) {
	root := t.TempDir()
	destinationCalls, sourceCalls := 0, 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls++
		_, _ = io.WriteString(w, `{"text":"wrong source"}`)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	if _, err := CallMemoryRecall(context.Background(), source.URL, root, "actual-call", MemoryRecall{}); err == nil || sourceCalls != 1 || destinationCalls != 0 {
		t.Fatalf("redirect source=%d destination=%d err=%v", sourceCalls, destinationCalls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CallMemoryRecall(ctx, source.URL, root, "actual-call", MemoryRecall{}); err == nil || sourceCalls != 1 {
		t.Fatalf("cancelled request sent source=%d err=%v", sourceCalls, err)
	}
}
