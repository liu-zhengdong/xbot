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

func TestLinksTypedTransportCallIDAndStrictBudgetObjects(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	searchCalls, loadCalls := 0, 0
	var gotSearch LinksSearch
	var gotLoad LinksLoad
	searchReply := LinksReply{Text: "browser\tavailable\tLocal Chrome"}
	loadReply := LinksReply{Text: "loaded browser: browser_open"}
	var sourceErr error
	source, err := Start(root, Handlers{Links: &LinksHandlers{
		Search: func(ctx context.Context, input LinksSearch) (LinksReply, error) {
			searchCalls++
			gotSearch = input
			if SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("search source call ID=%q", SourceCallID(ctx))
			}
			return searchReply, sourceErr
		},
		Load: func(ctx context.Context, input LinksLoad) (LinksReply, error) {
			loadCalls++
			gotLoad = input
			if SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("load source call ID=%q", SourceCallID(ctx))
			}
			return loadReply, sourceErr
		},
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
	request := func(method, target, body, givenRoot, origin, fetchSite, host string) (int, LinksReply) {
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
		raw, err := io.ReadAll(io.LimitReader(response.Body, LinksMaxReply+1))
		if err != nil || len(raw) > LinksMaxReply || !strings.HasSuffix(string(raw), "\n") {
			t.Fatalf("HTTP reply bytes=%d err=%v", len(raw), err)
		}
		var reply LinksReply
		if err := json.Unmarshal(raw, &reply); err != nil {
			t.Fatalf("invalid JSON reply=%q err=%v", raw, err)
		}
		return response.StatusCode, reply
	}
	for _, input := range []LinksSearch{{}, {Query: "browser"}, {Query: " \n--help\t "}, {Query: strings.Repeat("界", 3000)}} {
		reply, err := CallLinksSearch(context.Background(), source.Endpoint, alias, "actual-original-call", input)
		if err != nil || reply != searchReply || gotSearch != input {
			t.Fatalf("typed search input changed got=%+v want=%+v reply=%+v err=%v", gotSearch, input, reply, err)
		}
	}
	for _, input := range []LinksLoad{{ID: "browser"}, {ID: "电脑操作"}, {ID: "mcp:playwright"}} {
		reply, err := CallLinksLoad(context.Background(), source.Endpoint, alias, "actual-original-call", input)
		if err != nil || reply != loadReply || gotLoad != input {
			t.Fatalf("typed load input changed got=%+v want=%+v reply=%+v err=%v", gotLoad, input, reply, err)
		}
	}
	before := searchCalls + loadCalls
	if _, err := CallLinksSearch(context.Background(), source.Endpoint, root, "", LinksSearch{}); err == nil || searchCalls+loadCalls != before {
		t.Fatalf("missing call ID reached source calls=%d err=%v", searchCalls+loadCalls, err)
	}
	if _, err := CallLinksLoad(context.Background(), source.Endpoint, root, "", LinksLoad{ID: "browser"}); err == nil || searchCalls+loadCalls != before {
		t.Fatalf("missing call ID reached load source calls=%d err=%v", searchCalls+loadCalls, err)
	}
	if _, err := CallLinksSearch(context.Background(), source.Endpoint, root, "actual-original-call", LinksSearch{Query: strings.Repeat("x", LinksMaxRequest+1)}); err == nil || searchCalls+loadCalls != before {
		t.Fatalf("oversized query reached source calls=%d err=%v", searchCalls+loadCalls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CallLinksSearch(ctx, source.Endpoint, root, "actual-original-call", LinksSearch{}); !errors.Is(err, context.Canceled) || searchCalls+loadCalls != before {
		t.Fatalf("caller cancellation lost calls=%d err=%v", searchCalls+loadCalls, err)
	}
	for _, target := range []string{"/v1/links/search", "/v1/links/load"} {
		for _, raw := range []string{
			"", "null", "[]", `"body"`, "true", "17", `{} {}`, `{} true`, `{} garbage`,
			`{"self":true}`, `{"call_id":"fake"}`, `{"id":7}`, `{"query":false}`,
			strings.Repeat(" ", LinksMaxRequest+1),
			`{"query":"` + strings.Repeat("<", LinksMaxRequest/6+1) + `"}`,
		} {
			status, reply := request(http.MethodPost, target, raw, canonical, "", "", "")
			if status != http.StatusBadRequest || reply.Text != "" || reply.Error == "" || searchCalls+loadCalls != before {
				t.Fatalf("strict %s body bytes=%d status=%d calls=%d reply=%+v", target, len(raw), status, searchCalls+loadCalls, reply)
			}
		}
	}
	for _, tc := range []struct {
		method, target, root, origin, fetchSite, host string
		status                                        int
	}{
		{http.MethodGet, "/v1/links/search", canonical, "", "", "", http.StatusMethodNotAllowed},
		{http.MethodPut, "/v1/links/load", canonical, "", "", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/links/search?", canonical, "", "", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/links/load?query=x", canonical, "", "", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/links/search", alias, "", "", "", http.StatusConflict},
		{http.MethodPost, "/v1/links/load", "", "", "", "", http.StatusConflict},
		{http.MethodPost, "/v1/links/search", canonical, "http://browser.example", "", "", http.StatusForbidden},
		{http.MethodPost, "/v1/links/load", canonical, "", "same-origin", "", http.StatusForbidden},
		{http.MethodPost, "/v1/links/search", canonical, "", "", "localhost:1234", http.StatusForbidden},
	} {
		status, reply := request(tc.method, tc.target, `{}`, tc.root, tc.origin, tc.fetchSite, tc.host)
		if status != tc.status || reply.Error == "" || searchCalls+loadCalls != before {
			t.Fatalf("guard target=%q status=%d want=%d calls=%d", tc.target, status, tc.status, searchCalls+loadCalls)
		}
	}
	for _, raw := range []string{`{}`, `{"query":" --help "} \n`} {
		raw = strings.ReplaceAll(raw, `\n`, "\n")
		status, reply := request(http.MethodPost, "/v1/links/search", raw, canonical, "", "", "")
		if status != http.StatusOK || reply != searchReply {
			t.Fatalf("domain-neutral body=%q status=%d reply=%+v", raw, status, reply)
		}
	}
	base := `{"query":"`
	exactQuery := strings.Repeat("x", LinksMaxRequest-len(base)-len(`"}`))
	exact := base + exactQuery + `"}`
	before = searchCalls + loadCalls
	for _, raw := range []string{exact, exact + " "} {
		status, _ := request(http.MethodPost, "/v1/links/search", raw, canonical, "", "", "")
		if len(raw) == LinksMaxRequest {
			if status != http.StatusOK || searchCalls+loadCalls != before+1 || gotSearch.Query != exactQuery {
				t.Fatalf("exact request status=%d calls=%d query bytes=%d", status, searchCalls+loadCalls, len(gotSearch.Query))
			}
		} else if status != http.StatusBadRequest || searchCalls+loadCalls != before+1 {
			t.Fatalf("raw max+1 status=%d calls=%d", status, searchCalls+loadCalls)
		}
	}
	searchReply, sourceErr = LinksReply{}, errors.New("actual load failed after tool listing")
	reply, err := CallLinksSearch(context.Background(), source.Endpoint, root, "actual-original-call", LinksSearch{})
	if err == nil || err.Error() != sourceErr.Error() || reply != (LinksReply{Error: sourceErr.Error()}) {
		t.Fatalf("late error fabricated text reply=%+v err=%v", reply, err)
	}
	searchReply, sourceErr = LinksReply{}, errors.New("actual load failure: "+strings.Repeat("\x00<&>", 12<<10))
	reply, err = CallLinksSearch(context.Background(), source.Endpoint, root, "actual-original-call", LinksSearch{})
	wire, marshalErr := json.Marshal(reply)
	if err == nil || marshalErr != nil || reply.Text != "" || reply.Error == "" || len(wire)+1 > linksOutputBudget || len(FormatLinks(reply)) > linksOutputBudget {
		t.Fatalf("large error reply JSONNL=%d err=%v", len(wire)+1, err)
	}
	searchReply, sourceErr = LinksReply{Text: strings.Repeat("x", linksOutputBudget+1)}, nil
	status, reply := request(http.MethodPost, "/v1/links/search", `{}`, canonical, "", "", "")
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
		if r.Method != http.MethodPost || r.URL.Path != "/v1/links/load" || r.URL.RawQuery != "" || r.Header.Get("X-XBot-Root") != canonical || r.Header.Get("X-XBot-Call-ID") != "actual-original-call" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("wrong method/path/source headers: method=%s path=%q root=%q call=%q", r.Method, r.URL.Path, r.Header.Get("X-XBot-Root"), r.Header.Get("X-XBot-Call-ID"))
		}
		var input LinksLoad
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input != (LinksLoad{ID: "browser"}) {
			t.Errorf("typed wire=%+v err=%v", input, err)
		}
		if redirect {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}
		_, _ = io.WriteString(w, peerBody)
	}))
	defer peer.Close()
	for _, body := range []string{`{"text":"loaded browser: browser_open"}`, `not JSON`, strings.Repeat(" ", LinksMaxReply+1), `{"text":"` + strings.Repeat("x", linksOutputBudget+1) + `"}`} {
		peerBody = body
		reply, err := CallLinksLoad(context.Background(), peer.URL, alias, "actual-original-call", LinksLoad{ID: "browser"})
		if (err == nil) != (body == `{"text":"loaded browser: browser_open"}`) {
			t.Fatalf("peer reply bytes=%d text bytes=%d err=%v", len(body), len(reply.Text), err)
		}
		if strings.Contains(body, strings.Repeat("x", linksOutputBudget+1)) && len(reply.Text) != linksOutputBudget+1 {
			t.Fatal("client silently clipped supplied Text")
		}
	}
	peerBody, redirect = `{}`, true
	if _, err := CallLinksLoad(context.Background(), peer.URL, root, "actual-original-call", LinksLoad{ID: "browser"}); err == nil || redirected != 0 || peerCalls != 5 {
		t.Fatalf("redirect/retry calls=%d redirected=%d err=%v", peerCalls, redirected, err)
	}
}

// install/remove share the same strict transport as search/load: the call ID
// header is mandatory, the input marshals exactly {"id":...}, the reply is
// the same Text/Error shape, and a denied or missing handler maps onto
// Error rather than a fabricated success.
func TestLinksTypedTransportInstallAndRemove(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	var gotInstall LinksInstall
	var gotRemove LinksRemove
	installCalls, removeCalls := 0, 0
	var handlerErr error
	source, err := Start(root, Handlers{Links: &LinksHandlers{
		Install: func(ctx context.Context, input LinksInstall) (LinksReply, error) {
			installCalls++
			gotInstall = input
			if SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("install source call ID=%q", SourceCallID(ctx))
			}
			return LinksReply{Text: "installed " + input.ID}, handlerErr
		},
		Remove: func(ctx context.Context, input LinksRemove) (LinksReply, error) {
			removeCalls++
			gotRemove = input
			if SourceCallID(ctx) != "actual-original-call" {
				t.Errorf("remove source call ID=%q", SourceCallID(ctx))
			}
			return LinksReply{Text: "removed " + input.ID}, handlerErr
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	reply, err := CallLinksInstall(context.Background(), source.Endpoint, root, "actual-original-call", LinksInstall{ID: "telegram"})
	if err != nil || reply != (LinksReply{Text: "installed telegram"}) || gotInstall != (LinksInstall{ID: "telegram"}) {
		t.Fatalf("install reply=%+v got=%+v err=%v", reply, gotInstall, err)
	}
	reply, err = CallLinksRemove(context.Background(), source.Endpoint, root, "actual-original-call", LinksRemove{ID: "browser"})
	if err != nil || reply != (LinksReply{Text: "removed browser"}) || gotRemove != (LinksRemove{ID: "browser"}) {
		t.Fatalf("remove reply=%+v got=%+v err=%v", reply, gotRemove, err)
	}
	// CallID is mandatory on both nouns.
	if _, err := CallLinksInstall(context.Background(), source.Endpoint, root, "", LinksInstall{ID: "x"}); err == nil || installCalls != 1 {
		t.Fatalf("missing call ID install calls=%d err=%v", installCalls, err)
	}
	if _, err := CallLinksRemove(context.Background(), source.Endpoint, root, "", LinksRemove{ID: "x"}); err == nil || removeCalls != 1 {
		t.Fatalf("missing call ID remove calls=%d err=%v", removeCalls, err)
	}
	// The wire body is exactly the input's JSON, nothing more.
	wire, _ := json.Marshal(LinksInstall{ID: "browser"})
	if string(wire) != `{"id":"browser"}` {
		t.Fatalf("install wire %s", wire)
	}
	// A handler error arrives as Error and a call error, never swallowed.
	handlerErr = errors.New("denied by user")
	reply, err = CallLinksInstall(context.Background(), source.Endpoint, root, "actual-original-call", LinksInstall{ID: "telegram"})
	if err == nil || reply.Error != "denied by user" {
		t.Fatalf("denied install reply=%+v err=%v", reply, err)
	}
	handlerErr = nil
	// An oversized reply is refused on both nouns.
	source.Close()
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-XBot-Root", canonical)
		_, _ = io.WriteString(w, `{"text":"`+strings.Repeat("x", linksOutputBudget+1)+`"}`)
	}))
	defer big.Close()
	if _, err := CallLinksInstall(context.Background(), big.URL, root, "actual-original-call", LinksInstall{ID: "x"}); err == nil {
		t.Fatal("oversized install reply accepted")
	}
	if _, err := CallLinksRemove(context.Background(), big.URL, root, "actual-original-call", LinksRemove{ID: "x"}); err == nil {
		t.Fatal("oversized remove reply accepted")
	}
}
