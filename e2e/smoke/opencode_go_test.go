//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/config"
)

// The real serve/API/tool loop uses the official Go host via a local scripted
// HTTP proxy. No request reaches OpenCode or requires real credentials.
func TestOpenCodeGoBinary(t *testing.T) {
	var mu sync.Mutex
	sessions := map[string]string{}
	calls := map[string]int{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "opencode.ai" || r.URL.Path != "/zen/go/v1/chat/completions" {
			t.Errorf("unexpected upstream: %s", r.URL)
			http.Error(w, "unexpected upstream", 400)
			return
		}
		session := r.Header.Get("x-opencode-session")
		if session == "" {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"type":"MissingSessionID","message":"Request is missing x-opencode-session"}}`)
			return
		}
		if !strings.HasPrefix(r.UserAgent(), "xbot/") || r.Header.Get("Authorization") != "Bearer smoke" {
			t.Error("missing client identity/authentication")
		}
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		if old := sessions[req.Model]; old != "" && old != session {
			t.Errorf("session changed for %s: %q -> %q", req.Model, old, session)
		}
		sessions[req.Model] = session
		calls[req.Model]++
		n := calls[req.Model]
		mu.Unlock()
		if n%2 == 1 {
			streamTool(w, fmt.Sprintf("notify_%d", n), "send_message_to_user", `{"text":"Go channel works"}`)
		} else {
			streamText(w, []string{"SILENT"})
		}
	}))
	defer proxy.Close()
	srv := Start(t, StartStub(t))
	srv.CreateBot("goone", "Reply briefly.")
	srv.CreateBot("gotwo", "Reply briefly.")
	srv.Stop()
	f, err := config.Load(srv.Root)
	if err != nil {
		t.Fatal(err)
	}
	f.Providers = append(f.Providers, config.Provider{Name: "opencode-go", Protocol: config.ChatCompletions, BaseURL: "http://opencode.ai/zen/go/v1", APIKeyEnv: "XBOT_API_KEY", Models: []config.ModelSpec{{Name: "goone"}, {Name: "gotwo"}}})
	if err := config.Save(srv.Root, f); err != nil {
		t.Fatal(err)
	}
	srv.env = envWith(srv.env, "HTTP_PROXY="+proxy.URL, "HTTPS_PROXY="+proxy.URL, "NO_PROXY=127.0.0.1,localhost")
	srv.Restart()
	for _, name := range []string{"goone", "gotwo"} {
		status, body := srv.do(http.MethodPut, "/api/bots/"+name+"/binding", map[string]string{"provider": "opencode-go", "model": name})
		if status != 200 {
			t.Fatalf("bind: %d %s", status, body)
		}
	}
	for _, name := range []string{"goone", "goone", "gotwo"} {
		started := time.Now()
		reply := srv.Send(name, "Please reply.")
		if reply.Error != "" || reply.Done != "Go channel works" {
			t.Fatalf("reply: %+v", reply)
		}
		t.Logf("%s real binary response: %s", name, time.Since(started))
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["goone"] != 4 || calls["gotwo"] != 2 || sessions["goone"] == sessions["gotwo"] {
		t.Fatalf("calls=%v sessions=%v", calls, sessions)
	}
}
