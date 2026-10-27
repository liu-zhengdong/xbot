package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
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
  names=['browser_snapshot'] if mode=='nonav' else ['browser_navigate','browser_close','browser_snapshot']
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
	for _, m := range []string{"navfail", "nonav"} {
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
