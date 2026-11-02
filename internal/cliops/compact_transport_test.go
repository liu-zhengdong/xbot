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

func TestCompactTypedTransportCallIDAndStrictBudgetObjects(t *testing.T) {
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
	var got Compact
	actual := CompactReply{Text: "compacted 1-12"}
	var sourceErr error
	source, err := Start(root, Handlers{Compact: func(ctx context.Context, input Compact) (CompactReply, error) {
		calls++
		got = input
		if SourceCallID(ctx) != "actual-original-call" {
			t.Errorf("source call ID=%q", SourceCallID(ctx))
		}
		return actual, sourceErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	request := func(method, target, body, givenRoot, origin, fetchSite, host string) (int, CompactReply) {
		t.Helper()
		r, err := http.NewRequest(method, source.Endpoint+target, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-XBot-Root", givenRoot)
		r.Header.Set("X-XBot-Call-ID", "actual-original-call")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", fetchSite)
		if host != "" {
			r.Host = host
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, CompactMaxReply+1))
		if err != nil || len(raw) > CompactMaxReply || !strings.HasSuffix(string(raw), "\n") {
			t.Fatalf("HTTP reply bytes=%d err=%v", len(raw), err)
		}
		var reply CompactReply
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatalf("invalid JSON reply=%q err=%v", raw, err)
		}
		return response.StatusCode, reply
	}
	for _, input := range []Compact{{From: 1, To: 12}, {From: 0, To: -7, Focus: " \n--help\t "}, {From: 9, To: 1, Focus: strings.Repeat("界", 3000)}} {
		reply, err := CallCompact(context.Background(), source.Endpoint, alias, "actual-original-call", input)
		if err != nil || reply != actual || got != input {
			t.Fatalf("typed input changed got=%+v want=%+v reply=%+v err=%v", got, input, reply, err)
		}
	}
	before := calls
	for _, input := range []Compact{{From: 1, To: 2}, {Focus: strings.Repeat("x", CompactMaxRequest+1)}, {Focus: strings.Repeat("<", CompactMaxRequest/6+1)}, {Focus: strings.Repeat("\x00", CompactMaxRequest/6+1)}} {
		callID := "actual-original-call"
		if input == (Compact{From: 1, To: 2}) {
			callID = ""
		}
		if _, err := CallCompact(context.Background(), source.Endpoint, root, callID, input); err == nil || calls != before {
			t.Fatalf("invalid call reached source focus bytes=%d calls=%d err=%v", len(input.Focus), calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CallCompact(ctx, source.Endpoint, root, "actual-original-call", Compact{}); !errors.Is(err, context.Canceled) || calls != before {
		t.Fatalf("caller cancellation lost calls=%d err=%v", calls, err)
	}
	for _, raw := range []string{
		"", "null", "[]", `"body"`, "true", "17", `{} {}`, `{} true`, `{} garbage`,
		`{"self":true}`, `{"focus_file":"path"}`, `{"call_id":"fake"}`, `{"summary":"fake"}`,
		`{"from":"1","to":2}`, `{"from":1.5,"to":2}`, `{"from":1e2,"to":2}`, `{"from":999999999999999999999999999999}`, `{"to":[]}`, `{"focus":false}`,
		strings.Repeat(" ", CompactMaxRequest+1),
		`{"from":1,"to":2,"focus":"` + strings.Repeat("<", CompactMaxRequest/6+1) + `"}`,
	} {
		status, reply := request(http.MethodPost, "/v1/compact", raw, canonical, "", "", "")
		if status != http.StatusBadRequest || reply.Text != "" || reply.Error == "" || calls != before {
			t.Fatalf("strict body bytes=%d status=%d calls=%d reply=%+v", len(raw), status, calls, reply)
		}
	}
	for _, tc := range []struct {
		method, target, root, origin, fetchSite, host string
		status                                        int
	}{
		{http.MethodGet, "/v1/compact", canonical, "", "", "", http.StatusMethodNotAllowed},
		{http.MethodPut, "/v1/compact", canonical, "", "", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/compact?", canonical, "", "", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/compact?from=1", canonical, "", "", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/compact", alias, "", "", "", http.StatusConflict},
		{http.MethodPost, "/v1/compact", "", "", "", "", http.StatusConflict},
		{http.MethodPost, "/v1/compact", canonical, "http://browser.example", "", "", http.StatusForbidden},
		{http.MethodPost, "/v1/compact", canonical, "", "same-origin", "", http.StatusForbidden},
		{http.MethodPost, "/v1/compact", canonical, "", "", "localhost:1234", http.StatusForbidden},
	} {
		status, reply := request(tc.method, tc.target, `{}`, tc.root, tc.origin, tc.fetchSite, tc.host)
		if status != tc.status || reply.Error == "" || calls != before {
			t.Fatalf("guard target=%q status=%d want=%d calls=%d", tc.target, status, tc.status, calls)
		}
	}
	for _, raw := range []string{`{}`, `{"from":0,"to":-1,"focus":" --help "} \n`} {
		// Whitespace after the one object is allowed; a second value is not.
		raw = strings.ReplaceAll(raw, `\n`, "\n")
		status, reply := request(http.MethodPost, "/v1/compact", raw, canonical, "", "", "")
		if status != http.StatusOK || reply != actual {
			t.Fatalf("domain-neutral body=%q status=%d reply=%+v", raw, status, reply)
		}
	}
	base := `{"from":0,"to":-1,"focus":""}`
	exactFocus := strings.Repeat("x", CompactMaxRequest-len(base))
	exact := `{"from":0,"to":-1,"focus":"` + exactFocus + `"}`
	before = calls
	for _, raw := range []string{exact, exact + " "} {
		status, _ := request(http.MethodPost, "/v1/compact", raw, canonical, "", "", "")
		if len(raw) == CompactMaxRequest {
			if status != http.StatusOK || calls != before+1 || got.Focus != exactFocus {
				t.Fatalf("exact request status=%d calls=%d focus bytes=%d", status, calls, len(got.Focus))
			}
		} else if status != http.StatusBadRequest || calls != before+1 {
			t.Fatalf("raw max+1 status=%d calls=%d", status, calls)
		}
	}
	actual, sourceErr = CompactReply{}, errors.New("actual state save failed after model usage")
	reply, err := CallCompact(context.Background(), source.Endpoint, root, "actual-original-call", Compact{From: 1, To: 12})
	if err == nil || err.Error() != sourceErr.Error() || reply != (CompactReply{Error: sourceErr.Error()}) {
		t.Fatalf("late error fabricated acknowledgement reply=%+v err=%v", reply, err)
	}
	actual, sourceErr = CompactReply{}, errors.New("actual state save failure: "+strings.Repeat("\x00<&>", 12<<10))
	reply, err = CallCompact(context.Background(), source.Endpoint, root, "actual-original-call", Compact{})
	wire, marshalErr := json.Marshal(reply)
	if err == nil || marshalErr != nil || reply.Text != "" || reply.Error == "" || len(wire)+1 > compactOutputBudget || len(FormatCompact(reply)) > compactOutputBudget {
		t.Fatalf("large error reply JSONNL=%d err=%v", len(wire)+1, err)
	}
	actual, sourceErr = CompactReply{Text: strings.Repeat("x", compactOutputBudget+1)}, nil
	status, reply := request(http.MethodPost, "/v1/compact", `{}`, canonical, "", "", "")
	if status != http.StatusInternalServerError || reply.Text != "" || reply.Error == "" {
		t.Fatalf("oversized text became success status=%d reply=%+v", status, reply)
	}

	// Inspect actual client headers and its original HTTP response ceiling against
	// a local peer that deliberately returns invalid or oversized wire responses.
	peerCalls, redirected := 0, 0
	var peerBody string
	redirect := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++ }))
	defer target.Close()
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerCalls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/compact" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("X-XBot-Call-ID") != "actual-original-call" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong method/path/source headers: method=%s path=%q root=%q call=%q", r.Method, r.URL.Path, r.Header.Get("X-XBot-Root"), r.Header.Get("X-XBot-Call-ID"))
		}
		var input Compact
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input != (Compact{From: 0, To: -1, Focus: "--help"}) {
			t.Errorf("typed wire=%+v err=%v", input, err)
		}
		if redirect {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}
		_, _ = io.WriteString(w, peerBody)
	}))
	defer peer.Close()
	for _, body := range []string{`{"text":"compacted 1-12"}`, `not JSON`, strings.Repeat(" ", CompactMaxReply+1), `{"text":"` + strings.Repeat("x", compactOutputBudget+1) + `"}`} {
		peerBody = body
		reply, err := CallCompact(context.Background(), peer.URL, alias, "actual-original-call", Compact{From: 0, To: -1, Focus: "--help"})
		if (err == nil) != (body == `{"text":"compacted 1-12"}`) {
			t.Fatalf("peer reply bytes=%d text bytes=%d err=%v", len(body), len(reply.Text), err)
		}
		if strings.Contains(body, strings.Repeat("x", compactOutputBudget+1)) && len(reply.Text) != compactOutputBudget+1 {
			t.Fatal("client silently clipped supplied Text")
		}
	}
	peerBody, redirect = `{}`, true
	if _, err := CallCompact(context.Background(), peer.URL, root, "actual-original-call", Compact{From: 0, To: -1, Focus: "--help"}); err == nil || redirected != 0 || peerCalls != 5 {
		t.Fatalf("redirect/retry calls=%d redirected=%d err=%v", peerCalls, redirected, err)
	}
}
