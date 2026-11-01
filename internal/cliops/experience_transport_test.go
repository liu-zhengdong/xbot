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

func TestExperienceRuntimeTransportWireWithoutCallID(t *testing.T) {
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
		if err != nil || r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-XBot-Call-ID") != "" {
			t.Errorf("request method=%s path=%s root=%q callID=%q err=%v", r.Method, r.URL.Path, r.Header.Get("X-XBot-Root"), r.Header.Get("X-XBot-Call-ID"), err)
		}
		switch r.URL.Path {
		case "/v1/experience/record":
			if string(raw) != `{"summary":"\u003c\u0026\u003e\n","focus":" topic ","from":-2,"to":-1}` {
				t.Errorf("record wire=%s", raw)
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ExperienceRecordReply{ID: 19, From: -2, To: -1, Error: "unconfirmed write"})
		case "/v1/experience/search":
			if string(raw) != `{"query":"\u003c\u0026\u003e\n","limit":-5}` {
				t.Errorf("search wire=%s", raw)
			}
			_ = json.NewEncoder(w).Encode(ExperienceSearchReply{Text: "original semantic text"})
		default:
			t.Errorf("unexpected path=%s", r.URL.Path)
		}
	}))
	defer source.Close()
	record, err := CallExperienceRecord(context.Background(), source.URL, alias, ExperienceRecord{Summary: "<&>\n", Focus: " topic ", From: -2, To: -1})
	if err == nil || record.ID != 19 || record.From != -2 || record.To != -1 || record.Recorded != nil {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	search, err := CallExperienceSearch(context.Background(), source.URL, alias, ExperienceSearch{Query: "<&>\n", Limit: -5})
	if err != nil || search.Text != "original semantic text" || calls != 2 {
		t.Fatalf("search=%+v calls=%d err=%v", search, calls, err)
	}
}

func TestExperienceRuntimeTransportStrictWire(t *testing.T) {
	for _, action := range []string{"record", "search"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			handlers := &ExperienceHandlers{
				Record: func(context.Context, ExperienceRecord) (ExperienceRecordReply, error) {
					calls++
					return ExperienceRecordReply{}, nil
				},
				Search: func(context.Context, ExperienceSearch) (ExperienceSearchReply, error) {
					calls++
					return ExperienceSearchReply{Text: "semantic"}, nil
				},
			}
			serve := func(w http.ResponseWriter, r *http.Request) {
				if action == "record" {
					serveExperienceRecord(w, r, handlers)
				} else {
					serveExperienceSearch(w, r, handlers)
				}
			}
			bad := []string{"", "null", "[]", `"body"`, `true`, `{} {}`, `{} true`, `{} garbage`, `{"call_id":"fake"}`, `{"self":true}`, `{"summary_file":"fake"}`, `{"count":1}`, strings.Repeat(" ", ExperienceMaxRequest+1)}
			field := "summary"
			if action == "search" {
				field = "query"
			}
			bad = append(bad, `{"`+field+`":[]}`, `{"`+field+`":true}`, `{"`+field+`":"`+strings.Repeat("<", ExperienceMaxRequest/6+1)+`"}`)
			for _, raw := range bad {
				w := httptest.NewRecorder()
				serve(w, httptest.NewRequest("POST", "/v1/experience/"+action, strings.NewReader(raw)))
				if w.Code != http.StatusBadRequest || calls != 0 {
					t.Fatalf("bad wire bytes=%d code=%d calls=%d", len(raw), w.Code, calls)
				}
				if action == "record" && !strings.Contains(w.Body.String(), `"recorded":null`) {
					t.Fatalf("error lacks explicit null: %s", w.Body)
				}
			}
			for _, request := range []*http.Request{
				httptest.NewRequest("GET", "/v1/experience/"+action, nil),
				httptest.NewRequest("PUT", "/v1/experience/"+action, strings.NewReader(`{}`)),
				httptest.NewRequest("POST", "/v1/experience/"+action+"?source=fake", strings.NewReader(`{}`)),
			} {
				w := httptest.NewRecorder()
				serve(w, request)
				if w.Code < 400 || calls != 0 {
					t.Fatalf("method/query accepted: code=%d calls=%d", w.Code, calls)
				}
			}
			// Neither omitted fields nor domain-invalid bounds/limits are wire syntax errors.
			raw := `{"summary":"","focus":"` + strings.Repeat("f", 20<<10) + `","from":-5,"to":-3}`
			if action == "search" {
				raw = `{"query":"` + strings.Repeat("q", 513) + `","limit":99}`
			}
			for _, body := range []string{`{}`, raw} {
				w := httptest.NewRecorder()
				serve(w, httptest.NewRequest("POST", "/v1/experience/"+action, strings.NewReader(body)))
				if w.Code != http.StatusOK {
					t.Fatalf("wire imposed domain rules: code=%d body=%s", w.Code, w.Body)
				}
			}
			base := `{"` + field + `":""}`
			exact := `{"` + field + `":"` + strings.Repeat("x", ExperienceMaxRequest-len(base)) + `"}`
			for _, body := range []string{exact, exact + " "} {
				w := httptest.NewRecorder()
				serve(w, httptest.NewRequest("POST", "/v1/experience/"+action, strings.NewReader(body)))
				want := http.StatusOK
				if len(body) > ExperienceMaxRequest {
					want = http.StatusBadRequest
				}
				if w.Code != want {
					t.Fatalf("request bytes=%d code=%d want=%d", len(body), w.Code, want)
				}
			}
			if calls != 3 {
				t.Fatalf("calls=%d want=3", calls)
			}
		})
	}
}

func TestExperienceRuntimeTransportMountGuardsAndOptionalHeader(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, handlers := range []*ExperienceHandlers{nil, {}} {
		source, err := Start(root, Handlers{Experiences: handlers})
		if err != nil {
			t.Fatal(err)
		}
		record, recordErr := CallExperienceRecord(context.Background(), source.Endpoint, root, ExperienceRecord{})
		search, searchErr := CallExperienceSearch(context.Background(), source.Endpoint, root, ExperienceSearch{})
		source.Close()
		if recordErr == nil || searchErr == nil || record.Error == "" || search.Error == "" || record.Recorded != nil {
			t.Fatalf("missing callbacks record=%+v search=%+v", record, search)
		}
	}
	calls := 0
	confirmed := true
	source, err := Start(root, Handlers{Experiences: &ExperienceHandlers{
		Record: func(ctx context.Context, input ExperienceRecord) (ExperienceRecordReply, error) {
			calls++
			if SourceCallID(ctx) != "" {
				t.Error("experience associated a call ID")
			}
			return ExperienceRecordReply{ID: 1, Recorded: &confirmed}, nil
		},
		Search: func(ctx context.Context, input ExperienceSearch) (ExperienceSearchReply, error) {
			calls++
			if SourceCallID(ctx) != "" {
				t.Error("experience associated a call ID")
			}
			return ExperienceSearchReply{Text: "no matching experiences"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, action := range []string{"record", "search"} {
		for _, tc := range []struct {
			root, host, header, value string
			status                    int
		}{
			{root: "wrong", status: 409}, {root: canonical, host: "localhost:1", status: 403},
			{root: canonical, header: "Origin", value: "http://127.0.0.1", status: 403},
			{root: canonical, header: "Sec-Fetch-Site", value: "same-origin", status: 403},
		} {
			r, err := http.NewRequest("POST", source.Endpoint+"/v1/experience/"+action, strings.NewReader(`{}`))
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
	}
	for _, action := range []string{"record", "search"} {
		for _, header := range []string{"", "stale-finished-call", "bad id!"} {
			r, _ := http.NewRequest("POST", source.Endpoint+"/v1/experience/"+action, strings.NewReader(`{}`))
			r.Header.Set("X-XBot-Root", canonical)
			if header != "" {
				r.Header.Set("X-XBot-Call-ID", header)
			}
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("callID=%q rejected: %d", header, response.StatusCode)
			}
		}
	}
	if calls != 6 {
		t.Fatalf("calls=%d want=6", calls)
	}
	for _, endpoint := range []string{"", "https://127.0.0.1:1", "http://localhost:1", "http://192.0.2.1:1", source.Endpoint + "/path", source.Endpoint + "?q=x", source.Endpoint + "#fragment", "http://user@127.0.0.1:1"} {
		if _, err := CallExperienceRecord(context.Background(), endpoint, root, ExperienceRecord{}); err == nil {
			t.Errorf("record endpoint accepted %q", endpoint)
		}
		if _, err := CallExperienceSearch(context.Background(), endpoint, root, ExperienceSearch{}); err == nil {
			t.Errorf("search endpoint accepted %q", endpoint)
		}
	}
}

func TestExperienceRuntimeTransportBudgetsAndPartialFacts(t *testing.T) {
	root := t.TempDir()
	calls := 0
	body := `{"from":7,"to":9,"recorded":null,"id":21,"error":"late failure"}`
	status := http.StatusBadRequest
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer source.Close()
	for _, input := range []ExperienceRecord{{Summary: strings.Repeat("x", ExperienceMaxRequest+1)}, {Summary: "x", Focus: strings.Repeat("<", ExperienceMaxRequest/6+1)}} {
		if _, err := CallExperienceRecord(context.Background(), source.URL, root, input); err == nil || calls != 0 {
			t.Fatal("oversized record sent")
		}
	}
	if _, err := CallExperienceSearch(context.Background(), source.URL, root, ExperienceSearch{Query: strings.Repeat("<", ExperienceMaxRequest/6+1)}); err == nil || calls != 0 {
		t.Fatal("oversized search sent")
	}
	record, err := CallExperienceRecord(context.Background(), source.URL, root, ExperienceRecord{})
	if err == nil || record.ID != 21 || record.From != 7 || record.To != 9 || record.Recorded != nil {
		t.Fatalf("partial record=%+v err=%v", record, err)
	}
	base := `{"id":21,"from":7,"to":9,"recorded":null,"error":""}`
	for _, size := range []int{ExperienceMaxReply, ExperienceMaxReply + 1} {
		body = strings.Replace(base, `"error":""`, `"error":"`+strings.Repeat("x", size-len(base))+`"`, 1)
		got, err := CallExperienceRecord(context.Background(), source.URL, root, ExperienceRecord{})
		if err == nil {
			t.Fatal("oversized/error record succeeded")
		}
		if size == ExperienceMaxReply && (got.ID != 21 || got.From != 7 || got.To != 9 || got.Recorded != nil || got.Error == "") {
			t.Fatalf("HTTP exact budget lost facts %+v", got)
		}
		if size > ExperienceMaxReply && !reflect.DeepEqual(got, ExperienceRecordReply{}) {
			t.Fatalf("HTTP max+1 decoded %+v", got)
		}
	}
	body, status = `{"file":"/current/results/search.json","access":"local","bytes":36712}`, http.StatusServiceUnavailable
	search, err := CallExperienceSearch(context.Background(), source.URL, root, ExperienceSearch{})
	if err == nil || search.File != "/current/results/search.json" || search.Access != "local" || search.Bytes != 36712 || search.Error == "" {
		t.Fatalf("partial metadata=%+v err=%v", search, err)
	}
	for _, raw := range []string{"not JSON", `{} {}`} {
		body = raw
		if _, err := CallExperienceRecord(context.Background(), source.URL, root, ExperienceRecord{}); err == nil {
			t.Fatalf("bad response accepted %s", raw)
		}
	}
	if calls != 6 {
		t.Fatalf("calls=%d want=6 with no retries", calls)
	}
	// Callback errors leave the actual returned ID/range and null acknowledgement.
	w := httptest.NewRecorder()
	serveExperienceRecord(w, httptest.NewRequest("POST", "/v1/experience/record", strings.NewReader(`{}`)), &ExperienceHandlers{Record: func(context.Context, ExperienceRecord) (ExperienceRecordReply, error) {
		return ExperienceRecordReply{ID: 31, From: 4, To: 8}, errors.New(strings.Repeat("\x00", 12<<10))
	}})
	var got ExperienceRecordReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.ID != 31 || got.From != 4 || got.To != 8 || got.Recorded != nil || w.Code != 400 || w.Body.Len() > 32<<10 || len(FormatExperienceRecord(got)) > 32<<10 {
		t.Fatalf("bounded callback facts=%+v bytes=%d err=%v", got, w.Body.Len(), err)
	}
}
