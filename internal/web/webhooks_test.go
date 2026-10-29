package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/inbound"
	"xbot/internal/link"
)

func TestWebhookAPI(t *testing.T) {
	root := t.TempDir()
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for name, owner := range map[string]string{"ada": "u1", "eve": "u2", "小助手": "u1"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{owner}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	in := &inbound.Server{Root: root}
	in.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	public := ""
	s.WebhookBase = func() (string, string) { return srv.URL, public }

	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}

	if code, out := call("GET", "/api/bots/ada/webhooks", ""); code != 200 || len(out["hooks"].([]any)) != 0 {
		t.Fatalf("empty list: %d %v", code, out)
	}
	for _, bad := range []string{`{"name":"GitHub"}`, `{"name":"git_hub"}`, `{"name":"-x"}`, `{"name":""}`} {
		if code, out := call("POST", "/api/bots/ada/webhooks", bad); code != 400 || !strings.Contains(out["error"].(string), "小写字母、数字和短横线") {
			t.Fatalf("bad name %s: %d %v", bad, code, out)
		}
	}
	if code, _ := call("POST", "/api/bots/ada/webhooks", `{"name":"gh","auth":"basic"}`); code != 400 {
		t.Fatalf("bad auth: %d", code)
	}
	if code, _ := call("POST", "/api/bots/eve/webhooks", `{"name":"gh"}`); code != 404 {
		t.Fatalf("someone else's bot: %d", code)
	}

	code, gh := call("POST", "/api/bots/ada/webhooks", `{"name":"github"}`)
	secret, _ := gh["secret"].(string)
	if code != 201 || gh["auth"] != "hmac" || len(secret) != 64 || gh["url"] != srv.URL+"/webhook/ada/github" || gh["url"] != gh["local_url"] || gh["public_url"] != nil {
		t.Fatalf("create: %d %v", code, gh)
	}
	b, _ := bot.Load(root, "ada")
	if b.Grants(link.NameWebhook) {
		t.Fatal("native webhook was granted")
	}
	if code, out := call("POST", "/api/bots/ada/webhooks", `{"name":"github"}`); code != 409 || out["error"] != "已有同名 Webhook" {
		t.Fatalf("duplicate: %d %v", code, out)
	}
	code, ci := call("POST", "/api/bots/ada/webhooks", `{"name":"ci","auth":"bearer"}`)
	if code != 201 || ci["auth"] != "bearer" {
		t.Fatalf("bearer: %d %v", code, ci)
	}

	// The handed-out address and secret really reach the bot.
	body := []byte(`{"ref":"main"}`)
	post := func(url string, header map[string]string) int {
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := post(gh["url"].(string), map[string]string{"X-XBot-Signature": inbound.SignHMAC(secret, body)}); c != 202 {
		t.Fatalf("signed post: %d", c)
	}
	if c := post(gh["url"].(string), map[string]string{"X-XBot-Signature": inbound.SignHMAC("wrong", body)}); c != 401 {
		t.Fatalf("bad signature: %d", c)
	}
	if c := post(ci["url"].(string), map[string]string{"Authorization": "Bearer " + ci["secret"].(string)}); c != 202 {
		t.Fatalf("token post: %d", c)
	}

	// Any bot name gives a valid address to copy.
	code, zh := call("POST", "/api/bots/"+url.PathEscape("小助手")+"/webhooks", `{"name":"zh","auth":"bearer"}`)
	if code != 201 || zh["url"] != srv.URL+"/webhook/%E5%B0%8F%E5%8A%A9%E6%89%8B/zh" {
		t.Fatalf("escaped url: %d %v", code, zh)
	}
	if c := post(zh["url"].(string), map[string]string{"Authorization": "Bearer " + zh["secret"].(string)}); c != 202 {
		t.Fatalf("escaped url post: %d", c)
	}

	// The list never carries a secret; the detail does, for the owner.
	public = "https://pipe.example/pipe/v1/hook/mini-1"
	code, list := call("GET", "/api/bots/ada/webhooks", "")
	hooks := list["hooks"].([]any)
	if code != 200 || len(hooks) != 2 {
		t.Fatalf("list: %d %v", code, list)
	}
	for _, h := range hooks {
		h := h.(map[string]any)
		if _, ok := h["secret"]; ok {
			t.Fatalf("list leaks a secret: %v", h)
		}
	}
	first := hooks[0].(map[string]any)
	if first["url"] != public+"/webhook/ada/github" || first["public_url"] != first["url"] || first["local_url"] != srv.URL+"/webhook/ada/github" {
		t.Fatalf("public url: %v", first)
	}
	if code, out := call("GET", "/api/bots/ada/webhooks/github", ""); code != 200 || out["secret"] != secret {
		t.Fatalf("detail: %d %v", code, out)
	}
	// The polled profile carries the gateway base, so the page notices it
	// come and go; once the standby drops, nothing hands out the public one.
	if _, prof := call("GET", "/api/bots/ada", ""); prof["webhook_public"] != public {
		t.Fatalf("profile with the gateway up: %v", prof["webhook_public"])
	}
	public = ""
	if _, prof := call("GET", "/api/bots/ada", ""); prof["webhook_public"] != "" {
		t.Fatalf("profile with the gateway down: %v", prof["webhook_public"])
	}
	if _, out := call("GET", "/api/bots/ada/webhooks/github", ""); out["url"] != out["local_url"] || out["public_url"] != nil {
		t.Fatalf("detail with the gateway down: %v", out)
	}
	if code, _ := call("GET", "/api/bots/ada/webhooks/nope", ""); code != 404 {
		t.Fatalf("missing detail: %d", code)
	}

	for _, name := range []string{"github", "ci"} {
		if code, _ := call("DELETE", "/api/bots/ada/webhooks/"+name, ""); code != 204 {
			t.Fatalf("remove %s: %d", name, code)
		}
	}
	if code, _ := call("DELETE", "/api/bots/ada/webhooks/github", ""); code != 404 {
		t.Fatalf("remove twice: %d", code)
	}
	if code, out := call("GET", "/api/bots/ada/webhooks", ""); code != 200 || len(out["hooks"].([]any)) != 0 {
		t.Fatalf("empty again: %d %v", code, out)
	}
	if c := post(srv.URL+"/webhook/ada/github", map[string]string{"X-XBot-Signature": inbound.SignHMAC(secret, body)}); c != 404 {
		t.Fatalf("removed hook still answers: %d", c)
	}
	b, _ = bot.Load(root, "ada")
	if b.Grants(link.NameWebhook) || len(b.Config().Webhooks) != 0 {
		t.Fatalf("remove must drop the names: %+v", b.Config())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("a secret reached the log")
	}
}
