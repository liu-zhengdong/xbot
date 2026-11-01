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
	"reflect"
	"strings"
	"testing"
)

func TestBotForkRuntimeTransportWire(t *testing.T) {
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
	actual := BotForkReply{Name: "child", Created: true, Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, Stage: "handoff_memory", Error: "profile full"}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, err := io.ReadAll(r.Body)
		if err != nil || r.Method != "POST" || r.URL.Path != "/v1/bot/fork" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("Content-Type") != "application/json" || string(raw) != `{"name":"child","role":"\u003c\u0026\u003e\n","group":"project"}` {
			t.Errorf("method=%s path=%s root=%q body=%s err=%v", r.Method, r.URL.Path, r.Header.Get("X-XBot-Root"), raw, err)
		}
		wantID := "original-prepared"
		if calls == 2 {
			wantID = ""
		}
		if r.Header.Get("X-XBot-Call-ID") != wantID {
			t.Errorf("call ID=%q want=%q", r.Header.Get("X-XBot-Call-ID"), wantID)
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(actual)
	}))
	defer source.Close()
	for _, id := range []string{"original-prepared", ""} {
		got, err := CallBotFork(context.Background(), source.URL, alias, id, BotFork{Name: "child", Role: "<&>\n", Group: "project"})
		if err == nil || !reflect.DeepEqual(got, actual) {
			t.Fatalf("reply=%+v err=%v", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d, expected one attempt per call", calls)
	}
}

func TestBotForkRuntimeTransportStrictWire(t *testing.T) {
	calls := 0
	callback := func(ctx context.Context, input BotFork) (BotForkReply, error) {
		calls++
		if SourceCallID(ctx) != "original" {
			t.Errorf("call ID=%q", SourceCallID(ctx))
		}
		return BotForkReply{Name: input.Name, Stage: "validation"}, nil
	}
	for _, raw := range []string{
		"", "null", "[]", `"body"`, `true`, `{} {}`, `{} true`, `{} garbage`,
		`{"name":[],"role":"body"}`, `{"name":"child","role":true}`, `{"name":"child","role":"body","group":[]}`,
		`{"name":"child","role":"body","source":"fake"}`, `{"name":"child","role":"body","call_id":"fake"}`,
		`{"name":"child","role":"body","role_file":"file"}`, `{"name":"child","role":"body","self":true}`,
		strings.Repeat(" ", BotForkMaxRequest+1), `{"name":"child","role":"` + strings.Repeat("<", BotForkMaxRequest/6+1) + `"}`,
	} {
		w := httptest.NewRecorder()
		serveBotFork(w, httptest.NewRequest("POST", "/v1/bot/fork", strings.NewReader(raw)), callback)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("raw bytes=%d code=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "/v1/bot/fork", nil),
		httptest.NewRequest("PUT", "/v1/bot/fork", strings.NewReader(`{}`)),
		httptest.NewRequest("POST", "/v1/bot/fork?source=fake", strings.NewReader(`{}`)),
	} {
		w := httptest.NewRecorder()
		serveBotFork(w, request, callback)
		if w.Code < 400 || calls != 0 {
			t.Fatalf("method/query accepted: code=%d calls=%d", w.Code, calls)
		}
	}
	// The domain alone validates empty/oversized duties, names and groups.
	for _, input := range []BotFork{{}, {Name: " \nchild/ ", Role: " \n" + strings.Repeat("x", 8193), Group: strings.Repeat("g", 49)}} {
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/v1/bot/fork", strings.NewReader(string(raw)))
		r.Header.Set("X-XBot-Call-ID", "original")
		w := httptest.NewRecorder()
		serveBotFork(w, r, callback)
		if w.Code != http.StatusOK {
			t.Fatalf("domain checked at wire: code=%d body=%s", w.Code, w.Body)
		}
	}
	base := `{"name":"child","role":""}`
	exact := `{"name":"child","role":"` + strings.Repeat("x", BotForkMaxRequest-len(base)) + `"}`
	for _, raw := range []string{exact, exact + " "} {
		r := httptest.NewRequest("POST", "/v1/bot/fork", strings.NewReader(raw))
		r.Header.Set("X-XBot-Call-ID", "original")
		w := httptest.NewRecorder()
		serveBotFork(w, r, callback)
		want := http.StatusOK
		if len(raw) > BotForkMaxRequest {
			want = http.StatusBadRequest
		}
		if w.Code != want {
			t.Fatalf("raw bytes=%d code=%d want=%d", len(raw), w.Code, want)
		}
	}
	if calls != 3 {
		t.Fatalf("calls=%d want=3", calls)
	}
}

func TestBotForkRuntimeTransportMountAndGuards(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, bots := range []*BotHandlers{nil, {}} {
		source, err := Start(root, Handlers{Bots: bots})
		if err != nil {
			t.Fatal(err)
		}
		reply, callErr := CallBotFork(context.Background(), source.Endpoint, root, "", BotFork{})
		source.Close()
		if callErr == nil || !strings.Contains(reply.Error, "bot-fork: runtime unavailable") {
			t.Fatalf("nil handlers reply=%+v err=%v", reply, callErr)
		}
	}
	calls := 0
	live, err := Start(root, Handlers{Bots: &BotHandlers{Fork: func(ctx context.Context, input BotFork) (BotForkReply, error) {
		calls++
		if SourceCallID(ctx) != "" || input != (BotFork{Name: "child", Role: "duties"}) {
			t.Errorf("optional call ID=%q input=%+v", SourceCallID(ctx), input)
		}
		return BotForkReply{Name: input.Name, Stage: "validation"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		root, host, header, value string
		status                    int
	}{
		{root: "wrong", status: 409},
		{root: canonical, host: "localhost:1", status: 403},
		{root: canonical, header: "Origin", value: "http://127.0.0.1", status: 403},
		{root: canonical, header: "Sec-Fetch-Site", value: "same-origin", status: 403},
	} {
		r, err := http.NewRequest("POST", live.Endpoint+"/v1/bot/fork", strings.NewReader(`{"name":"child","role":"duties"}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-XBot-Root", tc.root)
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
	for _, endpoint := range []string{"", "https://127.0.0.1:1", "http://localhost:1", "http://192.0.2.1:1", live.Endpoint + "/path", live.Endpoint + "?q=x", live.Endpoint + "#fragment", "http://user@127.0.0.1:1"} {
		if _, err := CallBotFork(context.Background(), endpoint, root, "", BotFork{}); err == nil {
			t.Fatalf("endpoint accepted: %q", endpoint)
		}
	}
	if _, err := CallBotFork(context.Background(), live.Endpoint, root, "", BotFork{Name: "child", Role: "duties"}); err != nil || calls != 1 {
		t.Fatalf("exact route calls=%d err=%v", calls, err)
	}
}

func TestBotForkRuntimeTransportRequestBudgets(t *testing.T) {
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(BotForkReply{Stage: "validation"})
	}))
	defer source.Close()
	root := t.TempDir()
	for _, input := range []BotFork{
		{Name: "child", Role: strings.Repeat("x", BotForkMaxRequest+1)},
		{Name: "child", Role: strings.Repeat("<", BotForkMaxRequest/6+1)},
		{Name: strings.Repeat("<", BotForkMaxRequest/6+1), Role: "x"},
		{Name: "child", Role: "x", Group: strings.Repeat("<", BotForkMaxRequest/6+1)},
	} {
		if _, err := CallBotFork(context.Background(), source.URL, root, "", input); err == nil || calls != 0 {
			t.Fatalf("oversized input sent: calls=%d err=%v", calls, err)
		}
	}
	base := `{"name":"child","role":""}`
	input := BotFork{Name: "child", Role: strings.Repeat("x", BotForkMaxRequest-len(base))}
	if _, err := CallBotFork(context.Background(), source.URL, root, "", input); err != nil || calls != 1 {
		t.Fatalf("exact encoded request calls=%d err=%v", calls, err)
	}
	input.Role += "x"
	if _, err := CallBotFork(context.Background(), source.URL, root, "", input); err == nil || calls != 1 {
		t.Fatalf("encoded max+1 sent calls=%d err=%v", calls, err)
	}
}

func TestBotForkRuntimeTransportReceiptBudgets(t *testing.T) {
	actual := BotForkReply{Name: strings.Repeat("\x00", 12<<10), Created: true, CreateStage: "persona", Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, GroupID: "g_actual", Stage: "handoff_memory", Error: strings.Repeat("e", 40<<10)}
	w := httptest.NewRecorder()
	serveBotFork(w, httptest.NewRequest("POST", "/v1/bot/fork", strings.NewReader(`{"name":"child","role":"duties"}`)), func(context.Context, BotFork) (BotForkReply, error) {
		return actual, errors.New(actual.Error)
	})
	var got BotForkReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 400 || w.Body.Len() > 32<<10 || len(FormatBotFork(got)) > 32<<10 || !strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatalf("code=%d bytes=%d err=%v", w.Code, w.Body.Len(), err)
	}
	actual.Name, actual.Error = got.Name, got.Error
	if !reflect.DeepEqual(got, actual) || got.Name != "" || got.Error == "" {
		t.Fatalf("facts lost: got=%+v want=%+v", got, actual)
	}
	var responseBody string
	status := http.StatusOK
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer source.Close()
	root := t.TempDir()
	base := `{"created":true,"handoff_parts":2,"handoff_total":9,"stage":"handoff_memory","error":""}`
	for _, size := range []int{BotForkMaxReply, BotForkMaxReply + 1} {
		responseBody = strings.Replace(base, `"error":""`, `"error":"`+strings.Repeat("x", size-len(base))+`"`, 1)
		reply, err := CallBotFork(context.Background(), source.URL, root, "", BotFork{})
		if err == nil {
			t.Fatalf("oversized/error receipt succeeded at %d", size)
		}
		if size == BotForkMaxReply && (!reply.Created || reply.HandoffParts != 2 || reply.HandoffTotal != 9 || reply.Stage != "handoff_memory" || reply.Error == "") {
			t.Fatalf("exact HTTP limit not decoded: %+v", reply)
		}
		if size > BotForkMaxReply && !reflect.DeepEqual(reply, BotForkReply{}) {
			t.Fatalf("HTTP max+1 decoded: %+v", reply)
		}
	}
	responseBody, status = `{"created":true,"handoff_parts":0,"handoff_total":2,"stage":"child_note"}`, http.StatusServiceUnavailable
	reply, err := CallBotFork(context.Background(), source.URL, root, "", BotFork{})
	if err == nil || !reply.Created || reply.Stage != "child_note" || reply.Error == "" {
		t.Fatalf("HTTP failure lost receipt: %+v err=%v", reply, err)
	}
	for _, raw := range []string{"not JSON", `{}`, `{} {}`} {
		responseBody = raw
		_, err := CallBotFork(context.Background(), source.URL, root, "", BotFork{})
		if err == nil {
			t.Fatalf("bad/status failure succeeded: %q", raw)
		}
	}
	if calls != 6 {
		t.Fatalf("calls=%d want=6 without retry", calls)
	}
}
