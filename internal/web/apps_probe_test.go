package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/mcp"
)

// The scripted npx stands in for @playwright/mcp: a handshake alone is not
// enough, the probe must load a page through browser_navigate.
func TestAppsRealMCPProbe(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := `#!/usr/bin/python3
import json,os,sys
mode=open(` + "'" + `MODE` + "'" + `).read()
for line in sys.stdin:
 m=json.loads(line)
 if m.get('method')=='initialize': result={'protocolVersion':'2024-11-05','capabilities':{},'serverInfo':{'name':'test','version':'1'}}
 elif m.get('method')=='tools/list':
  names=['browser_snapshot'] if mode=='nonav' else ['prefix_browser_navigate','browser_close'] if mode=='suffix' else ['browser_navigate','browser_close','browser_snapshot']
  result={'tools':[{'name':n,'inputSchema':{'type':'object'}} for n in names]}
 elif m.get('method')=='tools/call':
  open(` + "'" + `CALLS` + "'" + `,'a').write(m['params']['name']+' '+json.dumps(m['params']['arguments'])+'\n')
  result={'content':[{'type':'text','text':'no browser'}],'isError':True} if mode=='navfail' and m['params']['name']=='browser_navigate' else {'content':[{'type':'text','text':'ok'}]}
 else: continue
 print(json.dumps({'jsonrpc':'2.0','id':m['id'],'result':result}),flush=True)
`
	mode := filepath.Join(dir, "mode")
	script = strings.NewReplacer("CALLS", calls, "MODE", mode).Replace(script)
	path := filepath.Join(dir, "npx")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	// spawnEnv keeps only a few variables, so the mode travels in a file.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	a, _ := link.LookupApp("playwright")
	s := &Server{}
	_ = os.WriteFile(mode, []byte("ok"), 0600)
	if err := s.probeApp(context.Background(), a, nil); err != nil {
		t.Fatalf("scripted browser: %v", err)
	}
	got, _ := os.ReadFile(calls)
	if !strings.HasPrefix(string(got), `browser_navigate {"url": "about:blank"}`) || !strings.Contains(string(got), "browser_close") {
		t.Fatalf("browser not driven: %q", got)
	}
	for _, m := range []string{"navfail", "nonav", "suffix"} {
		_ = os.WriteFile(mode, []byte(m), 0600)
		if err := s.probeApp(context.Background(), a, nil); err == nil {
			t.Fatalf("%s accepted", m)
		}
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.probeApp(context.Background(), a, nil); err == nil {
		t.Fatal("failed process accepted")
	}
}

func TestAppsHTTPProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	if err := probeHTTP(context.Background(), srv.URL, "fixture", true); err != nil {
		t.Fatal(err)
	}
	if err := probeHTTP(context.Background(), srv.URL, "wrong", true); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestAppsCancelledProbeDoesNotInstall(t *testing.T) {
	e := newAppsEnv(t)
	entered := make(chan struct{})
	e.server.appsProbe = func(ctx context.Context, _ link.App, _ []string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "POST", e.srv.URL+"/api/apps", strings.NewReader(`{"id":"context7","secrets":["fixture"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: e.tok})
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, _ := e.srv.Client().Do(req)
		if res != nil {
			res.Body.Close()
		}
	}()
	<-entered
	cancel()
	<-done
	// The mutation lock waits for the handler to observe cancellation and return.
	e.server.appsMu.Lock()
	e.server.appsMu.Unlock()
	if n, _ := e.list(); n != 0 {
		t.Fatal("cancelled install was committed")
	}
	if _, err := os.Stat(bot.GlobalMCPPath(e.root)); !os.IsNotExist(err) {
		t.Fatal("cancelled install wrote config")
	}
}

// Feishu checks the hook token before the body, so an unknown msg_type tells a
// good token from a bad one and can never reach a group.
func TestAppsLarkProbeSendsNothing(t *testing.T) {
	var bodies []string
	code := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.URL.Path+" "+string(raw))
		_, _ = fmt.Fprintf(w, `{"code":%d,"msg":"x"}`, code)
	}))
	defer srv.Close()
	for want, codes := range map[bool][]int{true: {19002, 9499}, false: {19001, 19021, 19022}} {
		for _, code = range codes {
			if err := probeLark(context.Background(), srv.URL, "hook-fixture"); (err == nil) != want {
				t.Fatalf("code %d: %v", code, err)
			}
		}
	}
	for _, b := range bodies {
		if b != `/open-apis/bot/v2/hook/hook-fixture {"msg_type":"xbot_probe"}` {
			t.Fatalf("probe could deliver: %s", b)
		}
	}
	if err := probeLark(context.Background(), srv.URL, "a/b"); err == nil {
		t.Fatal("path in token accepted")
	}
}

func TestAppsMCPProbePendingValuesAndCleanup(t *testing.T) {
	t.Setenv("TOKEN", "fixture-old-environment")
	var initializations, legacyLookup atomic.Int32
	var mu sync.Mutex
	var calls []string
	streamStarted, streamClosed := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("TOKEN") != "fixture-pending-value" {
			t.Error("probe did not use pending form credentials")
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			close(streamStarted)
			<-r.Context().Done()
			close(streamClosed)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Mcp-Session-Id", "probe-only")
		var result any = map[string]any{}
		switch msg.Method {
		case "initialize":
			initializations.Add(1)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			select {
			case <-streamStarted:
			case <-r.Context().Done():
				return
			}
			result = map[string]any{"tools": []any{map[string]any{"name": "browser_navigate"}, map[string]any{"name": "browser_close"}}}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			calls = append(calls, params.Name)
			mu.Unlock()
			if params.Name == "browser_navigate" && string(params.Arguments) != `{"url":"about:blank"}` {
				t.Error("probe changed navigation arguments")
			}
			// Closing the temporary browser stays best effort, including raw isError.
			result = map[string]any{"isError": params.Name == "browser_close", "content": []any{map[string]any{"type": "text", "text": "fixture-pending-value"}}}
		default:
			t.Error("unexpected method", msg.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := probeMCP(ctx, mcp.Spec{Name: "playwright", URL: srv.URL, SecretEnvs: []string{"TOKEN"}, Secret: func(string) (string, bool) { legacyLookup.Add(1); return "fixture-old-vault", true }}, map[string]string{"TOKEN": "fixture-pending-value"})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	if len(gotCalls) != 2 || gotCalls[0] != "browser_navigate" || gotCalls[1] != "browser_close" {
		t.Fatal(gotCalls)
	}
	if initializations.Load() != 1 || legacyLookup.Load() != 0 {
		t.Fatal("probe reused/reopened a session or read old credentials")
	}
	select {
	case <-streamClosed:
	case <-time.After(time.Second):
		t.Fatal("temporary probe did not close its inbound stream")
	}
}

func TestAppsMCPProbeMissingPendingValueNeverUsesEnvironment(t *testing.T) {
	t.Setenv("TOKEN", "fixture-environment")
	var dials, legacy atomic.Int32
	err := probeMCP(context.Background(), mcp.Spec{Name: "probe", SecretEnvs: []string{"TOKEN"}, Secret: func(string) (string, bool) { legacy.Add(1); return "fixture-vault", true }, Dial: func(context.Context) (io.ReadWriteCloser, error) {
		dials.Add(1)
		return nil, errors.New("unexpected dial")
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "TOKEN") || dials.Load() != 0 || legacy.Load() != 0 {
		t.Fatalf("error=%v dials=%d legacy=%d", err, dials.Load(), legacy.Load())
	}
}
