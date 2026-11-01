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
