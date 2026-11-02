package cliops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptClientsTypedRoutesCanonicalRootAndNoCallID(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_CALL_ID", "foreign-finished-call")
	var paths []string
	oversizedReply := false
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if oversizedReply {
			_, _ = io.WriteString(w, strings.Repeat(" ", TranscriptMaxReply+1))
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("X-XBot-Call-ID") != "" {
			t.Errorf("wrong source headers: root=%q call=%q", r.Header.Get("X-XBot-Root"), r.Header.Get("X-XBot-Call-ID"))
		}
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v1/transcript/search":
			if fields["query"] != "<&>\n" || fields["bot"] != " target " || len(fields) != 2 {
				t.Errorf("search input changed: %#v", fields)
			}
		case "/v1/transcript/expand":
			if fields["from"] != float64(-2) || fields["to"] != float64(0) || len(fields) != 2 {
				t.Errorf("expand input changed: %#v", fields)
			}
		case "/v1/transcript/detail":
			if fields["seq"] != float64(0) || fields["experience"] != float64(-1) || len(fields) != 2 {
				t.Errorf("detail input changed: %#v", fields)
			}
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(TranscriptReadReply{Text: "original semantic text"})
	}))
	defer source.Close()
	for _, call := range []func() (TranscriptReadReply, error){
		func() (TranscriptReadReply, error) {
			return CallTranscriptSearch(context.Background(), source.URL, alias, TranscriptSearch{Query: "<&>\n", Bot: " target "})
		},
		func() (TranscriptReadReply, error) {
			return CallTranscriptExpand(context.Background(), source.URL, alias, TranscriptExpand{From: -2})
		},
		func() (TranscriptReadReply, error) {
			return CallTranscriptDetail(context.Background(), source.URL, alias, TranscriptDetail{Experience: -1})
		},
	} {
		if reply, err := call(); err != nil || reply.Text != "original semantic text" {
			t.Fatalf("reply=%+v err=%v", reply, err)
		}
	}
	if len(paths) != 3 {
		t.Fatalf("requests=%d want=3 without retries", len(paths))
	}
	oversizedReply = true
	if reply, err := CallTranscriptSearch(context.Background(), source.URL, alias, TranscriptSearch{}); err == nil || reply != (TranscriptReadReply{}) || len(paths) != 4 {
		t.Fatalf("oversize HTTP response accepted or retried: calls=%d err=%v", len(paths), err)
	}
}

func TestTranscriptHTTPStrictObjectsAndRequestBudgets(t *testing.T) {
	calls := 0
	handlers := &TranscriptHandlers{
		Search: func(_ context.Context, in TranscriptSearch) (TranscriptReadReply, error) {
			calls++
			return TranscriptReadReply{Text: in.Query}, nil
		},
		Expand: func(_ context.Context, in TranscriptExpand) (TranscriptReadReply, error) {
			calls++
			if in.From != 0 || in.To != 0 {
				t.Errorf("missing range changed: %+v", in)
			}
			return TranscriptReadReply{Text: "original range validation"}, nil
		},
		Detail: func(_ context.Context, in TranscriptDetail) (TranscriptReadReply, error) {
			calls++
			if in.Seq != 0 || in.Experience != 0 {
				t.Errorf("missing seq changed: %+v", in)
			}
			return TranscriptReadReply{Text: "original seq validation"}, nil
		},
	}
	for _, serve := range []func(http.ResponseWriter, *http.Request, *TranscriptHandlers){serveTranscriptSearch, serveTranscriptExpand, serveTranscriptDetail} {
		before := calls
		for _, body := range []string{"null", "[]", `"text"`, `{} {}`, `{"extra":1}`} {
			w := httptest.NewRecorder()
			serve(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), handlers)
			if w.Code != 400 || calls != before {
				t.Fatalf("invalid object reached callback: body=%q code=%d", body, w.Code)
			}
		}
		for _, url := range []string{"/?query=x", "/?"} {
			w := httptest.NewRecorder()
			serve(w, httptest.NewRequest("POST", url, strings.NewReader(`{}`)), handlers)
			if w.Code != 400 || calls != before {
				t.Fatalf("URL query accepted: %s", url)
			}
		}
		w := httptest.NewRecorder()
		serve(w, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), handlers)
		if w.Code != 200 || calls != before+1 {
			t.Fatalf("missing fields did not reach domain: code=%d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	serveTranscriptDetail(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"seq":1,"bot":"other"}`)), handlers)
	if w.Code != 400 || calls != 3 {
		t.Fatal("detail accepted target Bot")
	}
	base := `{"query":""}`
	exact := `{"query":"` + strings.Repeat("x", TranscriptMaxRequest-len(base)) + `"}`
	for _, tc := range []struct {
		body     string
		accepted bool
	}{
		{exact, true}, {exact + " ", false},
		{`{"query":"` + strings.Repeat("<", TranscriptMaxRequest/6+1) + `"}`, false},
	} {
		before := calls
		w := httptest.NewRecorder()
		serveTranscriptSearch(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)), handlers)
		// Oversized semantic output may fail its display budget after the valid
		// request reaches the source; rejection must happen before that callback.
		if (calls == before+1) != tc.accepted {
			t.Fatalf("request bytes=%d callback=%v want=%v", len(tc.body), calls > before, tc.accepted)
		}
	}
}

func TestTranscriptMountedGuardsAndIgnoredCallID(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := Start(root, Handlers{Transcript: &TranscriptHandlers{Search: func(ctx context.Context, _ TranscriptSearch) (TranscriptReadReply, error) {
		calls++
		if SourceCallID(ctx) != "" {
			t.Error("transcript introduced a source-call context")
		}
		return TranscriptReadReply{Text: "no matches"}, nil
	}}})
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
		{root: "wrong", status: 409},
		{root: canonical, host: "localhost:1", status: 403},
		{root: canonical, header: "Origin", value: "http://127.0.0.1", status: 403},
		{root: canonical, header: "Sec-Fetch-Site", value: "same-origin", status: 403},
	} {
		r, err := http.NewRequest("POST", source.Endpoint+"/v1/transcript/search", strings.NewReader(`{}`))
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
			t.Fatalf("guard code=%d calls=%d", response.StatusCode, calls)
		}
	}
	for _, id := range []string{"", "finished-foreign-call"} {
		r, _ := http.NewRequest("POST", source.Endpoint+"/v1/transcript/search", strings.NewReader(`{}`))
		r.Header.Set("X-XBot-Root", canonical)
		r.Header.Set("X-XBot-Call-ID", id)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("callID=%q code=%d", id, response.StatusCode)
		}
	}
	if _, err := CallTranscriptSearch(context.Background(), "", root, TranscriptSearch{}); err == nil || calls != 2 {
		t.Fatal("missing endpoint fell through")
	}
}
