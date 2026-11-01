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

func TestPeerMessageRuntimeTransportWire(t *testing.T) {
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
	recorded := false
	actual := PeerMessageReply{Bot: "B@Laptop", Route: "remote", EnvelopeID: "env-actual", Gateway: "unknown", Queued: true, Stage: "record", Recorded: &recorded, Error: "record failed"}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, err := io.ReadAll(r.Body)
		if err != nil || r.Method != "POST" || r.URL.Path != "/v1/peer/message" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("Content-Type") != "application/json" || string(raw) != `{"bot":"B@Laptop","text":"\u003c\u0026\u003e\n"}` {
			t.Errorf("method=%s path=%s root=%q body=%s err=%v", r.Method, r.URL.Path, r.Header.Get("X-XBot-Root"), raw, err)
		}
		wantCallID := "original-prepared"
		if calls == 2 {
			wantCallID = ""
		}
		if r.Header.Get("X-XBot-Call-ID") != wantCallID {
			t.Errorf("call ID=%q want=%q", r.Header.Get("X-XBot-Call-ID"), wantCallID)
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(actual)
	}))
	defer source.Close()
	for _, id := range []string{"original-prepared", ""} {
		got, err := CallPeerMessage(context.Background(), source.URL, alias, id, PeerMessage{Bot: "B@Laptop", Text: "<&>\n"})
		if err == nil || !reflect.DeepEqual(got, actual) {
			t.Fatalf("reply=%+v err=%v", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d, expected one attempt per call", calls)
	}
}

func TestPeerMessageRuntimeTransportStrictWire(t *testing.T) {
	calls := 0
	callback := func(ctx context.Context, input PeerMessage) (PeerMessageReply, error) {
		calls++
		if SourceCallID(ctx) != "original" {
			t.Errorf("source call=%q", SourceCallID(ctx))
		}
		return PeerMessageReply{Bot: input.Bot, Route: "local", Posted: true, EventID: "event-actual", Stage: "complete"}, nil
	}
	for _, raw := range []string{
		"", "null", "[]", `"body"`, `true`, `{} {}`, `{} true`, `{"bot":"B","text":true}`, `{"bot":[],"text":"body"}`,
		`{"bot":"B","text":"body","source":"fake"}`, `{"bot":"B","text":"body","call_id":"fake"}`,
		strings.Repeat(" ", PeerMessageMaxRequest+1), `{"bot":"B","text":"` + strings.Repeat("<", PeerMessageMaxRequest/6+1) + `"}`,
	} {
		w := httptest.NewRecorder()
		servePeerMessage(w, httptest.NewRequest("POST", "/v1/peer/message", strings.NewReader(raw)), callback)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("request bytes=%d code=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, request := range []*http.Request{
		httptest.NewRequest("GET", "/v1/peer/message", nil),
		httptest.NewRequest("POST", "/v1/peer/message?source=fake", strings.NewReader(`{}`)),
	} {
		w := httptest.NewRecorder()
		servePeerMessage(w, request, callback)
		if w.Code < 400 || calls != 0 {
			t.Fatalf("method/query accepted: code=%d calls=%d", w.Code, calls)
		}
	}
	// Domain validation belongs to the original writers, including empty and
	// 8193-byte texts. This boundary only freezes and bounds the typed request.
	for _, input := range []PeerMessage{{}, {Bot: " \nB@unknown ", Text: " \n" + strings.Repeat("x", 8193)}} {
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/v1/peer/message", strings.NewReader(string(raw)))
		r.Header.Set("X-XBot-Call-ID", "original")
		w := httptest.NewRecorder()
		servePeerMessage(w, r, callback)
		if w.Code != http.StatusOK {
			t.Fatalf("domain checked at wire: code=%d body=%s", w.Code, w.Body)
		}
	}
	base := `{"bot":"B","text":""}`
	exact := `{"bot":"B","text":"` + strings.Repeat("x", PeerMessageMaxRequest-len(base)) + `"}`
	for _, raw := range []string{exact, exact + " "} {
		r := httptest.NewRequest("POST", "/v1/peer/message", strings.NewReader(raw))
		r.Header.Set("X-XBot-Call-ID", "original")
		w := httptest.NewRecorder()
		servePeerMessage(w, r, callback)
		want := http.StatusOK
		if len(raw) > PeerMessageMaxRequest {
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

func TestPeerMessageRuntimeTransportNilAndGuards(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	var unavailable func(context.Context, PeerMessage) (PeerMessageReply, error)
	source, err := Start(root, Handlers{PeerMessage: unavailable})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if reply, err := CallPeerMessage(context.Background(), source.Endpoint, root, "", PeerMessage{Bot: "B", Text: "body"}); err == nil || !strings.Contains(reply.Error, "unavailable") {
		t.Fatalf("typed nil reply=%+v err=%v", reply, err)
	}
	calls := 0
	live, err := Start(root, Handlers{PeerMessage: func(context.Context, PeerMessage) (PeerMessageReply, error) {
		calls++
		return PeerMessageReply{Route: "local", Stage: "validation"}, nil
	}})
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
		r, err := http.NewRequest("POST", live.Endpoint+"/v1/peer/message", strings.NewReader(`{"bot":"B","text":"body"}`))
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
	for _, endpoint := range []string{"https://127.0.0.1:1", "http://localhost:1", "http://192.0.2.1:1", live.Endpoint + "/path", live.Endpoint + "?q=x", live.Endpoint + "#fragment", "http://user@127.0.0.1:1"} {
		if _, err := CallPeerMessage(context.Background(), endpoint, root, "", PeerMessage{}); err == nil {
			t.Fatalf("endpoint accepted: %q", endpoint)
		}
	}
}

func TestPeerMessageRuntimeTransportReceiptBounds(t *testing.T) {
	recorded := false
	actual := PeerMessageReply{Bot: strings.Repeat("\x00", 12<<10), Route: "remote", Posted: true, EventID: "event-actual", EnvelopeID: "env-actual", Gateway: "accepted", GatewayStatus: 201, Queued: true, Recorded: &recorded, RecordSeq: 123, Stage: "record", Error: strings.Repeat("e", 40<<10)}
	w := httptest.NewRecorder()
	servePeerMessage(w, httptest.NewRequest("POST", "/v1/peer/message", strings.NewReader(`{"bot":"B","text":"body"}`)), func(context.Context, PeerMessage) (PeerMessageReply, error) {
		return actual, errors.New(actual.Error)
	})
	var got PeerMessageReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 400 || w.Body.Len() > 32<<10 || len(FormatPeerMessage(got)) > 32<<10 {
		t.Fatalf("code=%d bytes=%d err=%v", w.Code, w.Body.Len(), err)
	}
	actual.Bot, actual.Error = got.Bot, got.Error
	if !reflect.DeepEqual(got, actual) || got.Error == "" {
		t.Fatalf("facts lost: got=%+v want=%+v", got, actual)
	}
	var responseBody string
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, responseBody)
	}))
	defer source.Close()
	root := t.TempDir()
	base := `{"route":"local","posted":false,"queued":false,"stage":"validation","error":""}`
	for _, size := range []int{PeerMessageMaxReply, PeerMessageMaxReply + 1} {
		responseBody = strings.Replace(base, `"error":""`, `"error":"`+strings.Repeat("x", size-len(base))+`"`, 1)
		reply, err := CallPeerMessage(context.Background(), source.URL, root, "", PeerMessage{Bot: "B", Text: "body"})
		if err == nil {
			t.Fatalf("oversized/error receipt succeeded at %d", size)
		}
		if size == PeerMessageMaxReply && (reply.Stage != "validation" || reply.Error == "") {
			t.Fatalf("exact HTTP limit was not decoded: %+v", reply)
		}
		if size > PeerMessageMaxReply && reply.Error != "" {
			t.Fatalf("HTTP max+1 was decoded: %+v", reply)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d want=2", calls)
	}
}
