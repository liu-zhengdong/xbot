package cliops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonaRuntimeTransportTypedBodyCallIDAndACK(t *testing.T) {
	root := t.TempDir()
	text := " \n完整职责 <&>\" " + strings.Repeat("p", 32<<10) + "\n "
	calls := 0
	source, err := Start(root, Handlers{Bots: &BotHandlers{Persona: func(ctx context.Context, input BotPersona) (BotPersonaReply, error) {
		calls++
		if input.Text != text || SourceCallID(ctx) != "original-call" {
			t.Errorf("raw bytes=%d call=%q", len(input.Text), SourceCallID(ctx))
		}
		if calls == 1 {
			return BotPersonaReply{Name: "nonprimary", Written: true, Effective: "next_wake"}, nil
		}
		return BotPersonaReply{Name: "nonprimary"}, errors.New("write failed")
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for _, written := range []bool{true, false} {
		reply, err := CallBotPersona(context.Background(), source.Endpoint, root, "original-call", BotPersona{Text: text})
		if reply.Name != "nonprimary" || reply.Written != written || (err == nil) != written || (reply.Effective == "next_wake") != written || (reply.Error == "") != written {
			t.Fatalf("written=%t reply=%+v error=%v", written, reply, err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if _, err := CallBotPersona(context.Background(), source.Endpoint, t.TempDir(), "original-call", BotPersona{Text: text}); err == nil || calls != 2 {
		t.Fatalf("root mismatch calls=%d error=%v", calls, err)
	}
	// Also inspect the actual wire contract instead of accepting a compatible
	// server decoder as evidence of the method, body keys or optional header.
	wireSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		want, _ := json.Marshal(BotPersona{Text: text})
		if r.Method != http.MethodPut || r.URL.RequestURI() != "/v1/bot/persona" || string(raw) != string(want) || r.Header.Get("X-XBot-Call-ID") != "" {
			t.Errorf("method=%s path=%s bytes=%d call=%q", r.Method, r.URL.RequestURI(), len(raw), r.Header.Get("X-XBot-Call-ID"))
		}
		_, _ = io.WriteString(w, `{"name":"nonprimary","written":true,"effective":"next_wake"}`)
	}))
	defer wireSource.Close()
	if _, err := CallBotPersona(context.Background(), wireSource.URL, root, "", BotPersona{Text: text}); err != nil {
		t.Fatal(err)
	}
}

func TestPersonaRuntimeTransportStrictBodyAndRequestBudget(t *testing.T) {
	calls := 0
	handlers := &BotHandlers{Persona: func(context.Context, BotPersona) (BotPersonaReply, error) {
		calls++
		return BotPersonaReply{Name: "source", Written: true, Effective: "next_wake"}, nil
	}}
	for _, raw := range []string{
		"null", "[]", `"text"`, `{} {}`, `{} true`, `{"unknown":1}`, `{"text":true}`, `{"name":"target","text":"x"}`,
		"\u00a0{}", "{}\u00a0", strings.Repeat(" ", BotMaxRequest+1),
		`{"text":"` + strings.Repeat("<", BotMaxRequest/6+1) + `"}`,
	} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/persona", strings.NewReader(raw)), handlers)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("body bytes=%d status=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	base := `{"text":""}`
	exact := `{"text":"` + strings.Repeat("x", BotMaxRequest-len(base)) + `"}`
	for _, raw := range []string{exact, exact + " "} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/persona", strings.NewReader(raw)), handlers)
		if (w.Code == http.StatusOK) != (len(raw) == BotMaxRequest) || calls != 1 {
			t.Fatalf("bytes=%d status=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(method, "/v1/bot/persona", strings.NewReader(`{"text":"x"}`)), handlers)
		if w.Code != http.StatusMethodNotAllowed || calls != 1 {
			t.Fatalf("method=%s status=%d calls=%d", method, w.Code, calls)
		}
	}
	// Empty and oversized domain duties still reach the sole domain validator.
	for _, text := range []string{"", " \n ", strings.Repeat("x", (32<<10)+1)} {
		raw, _ := json.Marshal(BotPersona{Text: text})
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/persona", strings.NewReader(string(raw))), handlers)
		if w.Code != http.StatusOK {
			t.Fatalf("domain input bytes=%d status=%d", len(text), w.Code)
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestPersonaRuntimeTransportGuards(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	source, err := Start(root, Handlers{Bots: &BotHandlers{Persona: func(context.Context, BotPersona) (BotPersonaReply, error) {
		calls++
		return BotPersonaReply{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ header, value string }{
		{"X-XBot-Root", "other-root"}, {"Origin", "http://localhost"}, {"Sec-Fetch-Site", "same-origin"}, {"Host", "localhost:1234"},
	} {
		req, _ := http.NewRequest(http.MethodPut, source.Endpoint+"/v1/bot/persona", strings.NewReader(`{"text":"x"}`))
		req.Header.Set("X-XBot-Root", canonical)
		if tc.header == "Host" {
			req.Host = tc.value
		} else {
			req.Header.Set(tc.header, tc.value)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 || calls != 0 {
			t.Fatalf("guard=%s status=%d calls=%d", tc.header, response.StatusCode, calls)
		}
	}
}

func TestPersonaRuntimeTransportExactReplyBudgetsAndWrittenFacts(t *testing.T) {
	reply := BotPersonaReply{Written: true, Effective: "next_wake"}
	base, _ := json.Marshal(reply)
	reply.Name = strings.Repeat("x", (32<<10)-len(base)-1)
	handlers := &BotHandlers{Persona: func(context.Context, BotPersona) (BotPersonaReply, error) { return reply, nil }}
	w := httptest.NewRecorder()
	serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/persona", strings.NewReader(`{"text":"x"}`)), handlers)
	if w.Code != http.StatusOK || w.Body.Len() != 32<<10 || !strings.HasSuffix(w.Body.String(), "\n") || len(FormatBotPersona(reply)) > 32<<10 {
		t.Fatalf("exact status=%d bytes=%d", w.Code, w.Body.Len())
	}
	for _, name := range []string{reply.Name + "x", strings.Repeat(`"`, 17000)} {
		reply.Name = name
		w = httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/persona", strings.NewReader(`{"text":"x"}`)), handlers)
		var got BotPersonaReply
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code == http.StatusOK || w.Body.Len() > 32<<10 || len(FormatBotPersona(got)) > 32<<10 || !got.Written || got.Effective != "next_wake" || got.Error == "" {
			t.Fatalf("overflow status=%d bytes=%d written=%t effective=%q error=%q", w.Code, w.Body.Len(), got.Written, got.Effective, got.Error)
		}
	}
	root := t.TempDir()
	for _, size := range []int{BotMaxReply, BotMaxReply + 1} {
		base := `{"name":"source","written":true,"effective":"next_wake"}`
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, base+strings.Repeat(" ", size-len(base)))
		}))
		_, err := CallBotPersona(context.Background(), source.URL, root, "", BotPersona{Text: "x"})
		source.Close()
		if (err == nil) != (size == BotMaxReply) {
			t.Fatalf("HTTP bytes=%d error=%v", size, err)
		}
	}
}

func TestBotControlTransportTypedQueriesAndPartial(t *testing.T) {
	root := t.TempDir()
	calls := 0
	source, err := Start(root, Handlers{Bots: &BotHandlers{
		Pin: func(ctx context.Context, input BotPin) (BotPinReply, error) {
			calls++
			if input.Name != "literal / Bot +&?" || input.Pinned || SourceCallID(ctx) != "" {
				t.Errorf("pin input=%+v call=%q", input, SourceCallID(ctx))
			}
			return BotPinReply{Name: input.Name, Pinned: input.Pinned, Stage: "config"}, errors.New("save failed")
		},
		Primary: func(ctx context.Context, name string) (BotPrimaryReply, error) {
			calls++
			if name != "literal / Bot +&?" || SourceCallID(ctx) != "" {
				t.Errorf("primary name=%q call=%q", name, SourceCallID(ctx))
			}
			return BotPrimaryReply{Name: name, Primary: name, Applied: true, Stage: "pin"}, errors.New("pin failed")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	pin, err := CallBotPin(context.Background(), source.Endpoint, root, BotPin{Name: "literal / Bot +&?", Pinned: false})
	if err == nil || err.Error() != "save failed" || pin.Pinned || pin.Applied || pin.Stage != "config" || pin.Error != err.Error() {
		t.Fatalf("pin=%+v error=%v", pin, err)
	}
	primary, err := CallBotPrimary(context.Background(), source.Endpoint, root, "literal / Bot +&?")
	if err == nil || err.Error() != "pin failed" || !primary.Applied || primary.Primary != "literal / Bot +&?" || primary.Stage != "pin" || primary.Error != err.Error() {
		t.Fatalf("primary=%+v error=%v", primary, err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want two without retry", calls)
	}
}

func TestBotControlTransportQueryShapeAndGuards(t *testing.T) {
	calls := 0
	handlers := &BotHandlers{
		Pin: func(_ context.Context, input BotPin) (BotPinReply, error) {
			calls++
			return BotPinReply{Name: input.Name, Pinned: input.Pinned, Applied: true, Stage: "complete"}, nil
		},
		Primary: func(_ context.Context, name string) (BotPrimaryReply, error) {
			calls++
			return BotPrimaryReply{Name: name, Primary: name, Applied: true, Stage: "complete"}, nil
		},
	}
	for _, path := range []string{
		"pin", "pin?name=a", "pin?name=a&pinned=", "pin?name=a&pinned=0", "pin?name=a&pinned=1", "pin?name=a&pinned=TRUE", "pin?name=a&pinned=False",
		"pin?name=a&pinned=false&pinned=false", "pin?name=a&name=b&pinned=true", "pin?name=&pinned=false", "pin?name=a&pinned=true&extra=x",
		"pin?name=%zz&pinned=true", "pin?name=a;pinned=true", "primary", "primary?name=", "primary?name=a&name=a", "primary?name=a&pinned=true",
		"pin?name=" + strings.Repeat("a", BotMaxRequest) + "&pinned=true",
	} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/"+path, nil), handlers)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("path prefix=%q status=%d calls=%d", path[:min(len(path), 100)], w.Code, calls)
		}
	}
	for _, action := range []string{"pin", "primary"} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodGet, "/v1/bot/"+action+"?name=a&pinned=true", nil), handlers)
		if w.Code != http.StatusMethodNotAllowed || calls != 0 {
			t.Fatalf("%s status=%d calls=%d", action, w.Code, calls)
		}
	}
	for _, value := range []string{"true", "false"} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPut, "/v1/bot/pin?name=a&pinned="+value, nil), handlers)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"pinned":`+value) {
			t.Fatalf("literal %s status=%d body=%s", value, w.Code, w.Body.String())
		}
	}
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Start(root, Handlers{Bots: handlers})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, action := range []string{"pin?name=a&pinned=false", "primary?name=a"} {
		for _, tc := range []struct{ header, value string }{{"X-XBot-Root", "other-root"}, {"Origin", "http://localhost"}, {"Sec-Fetch-Site", "same-origin"}, {"Host", "localhost:1234"}} {
			req, _ := http.NewRequest(http.MethodPut, source.Endpoint+"/v1/bot/"+action, nil)
			req.Header.Set("X-XBot-Root", canonical)
			if tc.header == "Host" {
				req.Host = tc.value
			} else {
				req.Header.Set(tc.header, tc.value)
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode < 400 || calls != 2 {
				t.Fatalf("guard=%s action=%s status=%d calls=%d", tc.header, action, response.StatusCode, calls)
			}
		}
	}
}

func TestBotTransportTypedRoutesAndPartial(t *testing.T) {
	root := t.TempDir()
	calls := 0
	local, remote, bots := []BotListLocal{}, []BotListRemote{}, []BotStatusView{}
	source, err := Start(root, Handlers{Bots: &BotHandlers{
		List: func(ctx context.Context) (BotListReply, error) {
			calls++
			if SourceCallID(ctx) != "" {
				t.Errorf("ordinary list sent call ID %q", SourceCallID(ctx))
			}
			return BotListReply{Local: &local, Remote: &remote}, nil
		},
		Info: func(ctx context.Context, query BotInfoQuery) (BotInfoReply, error) {
			calls++
			if SourceCallID(ctx) != "" || query.Bot != "literal / Bot" || query.Steps != 99 {
				t.Errorf("info query=%+v call=%q", query, SourceCallID(ctx))
			}
			return BotInfoReply{Bots: &bots}, nil
		},
		Create: func(ctx context.Context, input BotCreate) (BotCreateReply, error) {
			calls++
			if SourceCallID(ctx) != "" || input.Name != "ada" || input.Persona != " raw " {
				t.Errorf("create input=%+v call=%q", input, SourceCallID(ctx))
			}
			return BotCreateReply{Name: input.Name, Applied: true, Stage: "persona"}, errors.New("persona write failed")
		},
		Delete: func(ctx context.Context, name string) (BotDeleteReply, error) {
			calls++
			if SourceCallID(ctx) != "source-call" || name != "literal / Bot" {
				t.Errorf("delete name=%q call=%q", name, SourceCallID(ctx))
			}
			return BotDeleteReply{Name: name, Removed: true, Stage: "vault_key"}, errors.New("vault key removal failed")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx := context.Background()
	listed, err := CallBotList(ctx, source.Endpoint, root)
	if err != nil || listed.Local == nil || *listed.Local == nil || listed.Remote == nil || *listed.Remote == nil {
		t.Fatalf("list=%+v error=%v", listed, err)
	}
	info, err := CallBotInfo(ctx, source.Endpoint, root, BotInfoQuery{Bot: "literal / Bot", Steps: 99})
	if err != nil || info.Bots == nil || *info.Bots == nil {
		t.Fatalf("info=%+v error=%v", info, err)
	}
	created, err := CallBotCreate(ctx, source.Endpoint, root, BotCreate{Name: "ada", Persona: " raw "})
	if err == nil || err.Error() != "persona write failed" || !created.Applied || created.Stage != "persona" || created.Error != err.Error() {
		t.Fatalf("create=%+v error=%v", created, err)
	}
	deleted, err := CallBotDelete(ctx, source.Endpoint, root, "source-call", "literal / Bot")
	if err == nil || err.Error() != "vault key removal failed" || !deleted.Removed || deleted.Stage != "vault_key" || deleted.Error != err.Error() {
		t.Fatalf("delete=%+v error=%v", deleted, err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d want four without retry", calls)
	}
}

func TestBotTransportRequestGuardsAndBudgets(t *testing.T) {
	calls := 0
	handlers := &BotHandlers{Create: func(context.Context, BotCreate) (BotCreateReply, error) {
		calls++
		return BotCreateReply{Stage: "complete"}, nil
	}}
	for _, raw := range []string{"null", "[]", `{} {}`, `{"unknown":1}`, `{"persona":true}`, "\u00a0{}", "{}\u00a0", strings.Repeat(" ", BotMaxRequest+1), `{"persona":"` + strings.Repeat("<", BotMaxRequest/2) + `"}`} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(http.MethodPost, "/v1/bot/create", strings.NewReader(raw)), handlers)
		if w.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("invalid body length=%d status=%d calls=%d", len(raw), w.Code, calls)
		}
	}
	for _, tc := range []struct{ action, method string }{{"list", http.MethodPost}, {"info", http.MethodPost}, {"create", http.MethodGet}, {"delete", http.MethodGet}} {
		w := httptest.NewRecorder()
		serveBot(w, httptest.NewRequest(tc.method, "/v1/bot/"+tc.action, nil), handlers)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s wrong method status=%d", tc.action, w.Code)
		}
	}
	w := httptest.NewRecorder()
	serveBot(w, httptest.NewRequest(http.MethodGet, "/v1/bot/info?steps=bad", nil), &BotHandlers{Info: func(context.Context, BotInfoQuery) (BotInfoReply, error) { calls++; return BotInfoReply{}, nil }})
	if w.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("invalid steps status=%d calls=%d", w.Code, calls)
	}
	base := `{"name":"ada","persona":""}`
	exact := `{"name":"ada","persona":"` + strings.Repeat("x", BotMaxRequest-len(base)) + `"}`
	w = httptest.NewRecorder()
	serveBot(w, httptest.NewRequest(http.MethodPost, "/v1/bot/create", strings.NewReader(exact)), handlers)
	if w.Code != http.StatusOK || calls != 1 {
		t.Fatalf("exact 1 MiB status=%d calls=%d bytes=%d", w.Code, calls, len(exact))
	}
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := Start(root, Handlers{Bots: handlers})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ header, value string }{{"X-XBot-Root", "other-root"}, {"Origin", "http://localhost"}, {"Sec-Fetch-Site", "same-origin"}, {"Host", "localhost:1234"}} {
		req, _ := http.NewRequest(http.MethodPost, source.Endpoint+"/v1/bot/create", strings.NewReader(`{}`))
		req.Header.Set("X-XBot-Root", canonical)
		if tc.header == "Host" {
			req.Host = tc.value
		} else {
			req.Header.Set(tc.header, tc.value)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 || calls != 1 {
			t.Fatalf("guard %s status=%d calls=%d", tc.header, response.StatusCode, calls)
		}
	}
}

func TestBotTransportReplyBoundaryAndNoRedirect(t *testing.T) {
	local := []BotListLocal{{Name: ""}}
	reply := BotListReply{Local: &local}
	base, _ := json.Marshal(reply)
	local[0].Name = strings.Repeat("x", BotMaxReply-len(base)-1)
	handlers := &BotHandlers{List: func(context.Context) (BotListReply, error) { return reply, nil }}
	w := httptest.NewRecorder()
	serveBot(w, httptest.NewRequest(http.MethodGet, "/v1/bot/list", nil), handlers)
	if w.Code != http.StatusOK || w.Body.Len() != BotMaxReply || !strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatalf("exact response status=%d bytes=%d", w.Code, w.Body.Len())
	}
	local[0].Name += "x"
	w = httptest.NewRecorder()
	serveBot(w, httptest.NewRequest(http.MethodGet, "/v1/bot/list", nil), handlers)
	if w.Code != http.StatusInternalServerError || w.Body.Len() > BotMaxReply {
		t.Fatalf("newline overflow status=%d bytes=%d", w.Code, w.Body.Len())
	}
	root := t.TempDir()
	for _, size := range []int{BotMaxReply, BotMaxReply + 1} {
		body := `{"count":0}` + strings.Repeat(" ", size-len(`{"count":0}`))
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
		_, err := CallBotList(context.Background(), source.URL, root)
		source.Close()
		if (err == nil) != (size == BotMaxReply) {
			t.Fatalf("client response bytes=%d error=%v", size, err)
		}
	}
	requests, redirects := 0, 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects++; _, _ = io.WriteString(w, `{}`) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	if _, err := CallBotCreate(context.Background(), source.URL, root, BotCreate{Name: "ada", Persona: "p"}); err == nil || requests != 1 || redirects != 0 {
		t.Fatalf("redirect error=%v requests=%d redirected=%d", err, requests, redirects)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CallBotList(ctx, source.URL, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source error=%v", err)
	}
}
