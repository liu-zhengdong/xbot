package inbound

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestConnectorInbound(t *testing.T) {
	const secret = "token-value-1"
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTelegram, link.NameLark})
	if err != nil {
		t.Fatal(err)
	}
	if err := link.WriteSecrets(b.SecretPath(), map[string]string{
		"TELEGRAM_BOT_TOKEN": secret,
		"LARK_APP_SECRET":    "lark-secret",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root}
	ui := httptest.NewServer(s.Handler())
	defer ui.Close()

	res := postConnector(t, ui.URL+"/connector/telegram/ada", secret, `{"update_id":7,"message":{"message_id":1,"from":{"id":4,"username":"ada"},"chat":{"id":42},"text":"hello"}}`)
	if res.status != http.StatusOK || !strings.Contains(res.body, `"accepted"`) {
		t.Fatalf("%d %s", res.status, res.body)
	}
	res = postConnector(t, ui.URL+"/connector/telegram/ada", secret, `{"update_id":7,"message":{"message_id":1,"from":{"id":4,"username":"ada"},"chat":{"id":42},"text":"hello"}}`)
	if res.status != http.StatusOK || !strings.Contains(res.body, `"duplicate"`) {
		t.Fatalf("dup %d %s", res.status, res.body)
	}
	res = postConnector(t, ui.URL+"/connector/telegram/ada", "nope", `{"update_id":8,"message":{"text":"x","chat":{"id":1}}}`)
	if res.status != http.StatusUnauthorized {
		t.Fatalf("auth %d %s", res.status, res.body)
	}
	res = postConnector(t, ui.URL+"/connector/telegram/missing", secret, `{"update_id":9,"message":{"text":"x","chat":{"id":1}}}`)
	if res.status != http.StatusNotFound {
		t.Fatalf("missing %d", res.status)
	}

	req, err := http.NewRequest(http.MethodPost, ui.URL+"/connector/lark/ada", strings.NewReader(`{"type":"url_verification","token":"lark-secret","challenge":"abc_1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if got.StatusCode != http.StatusOK || !strings.Contains(string(buf), `"challenge":"abc_1"`) || strings.Contains(string(buf), "lark-secret") {
		t.Fatalf("challenge %d %s", got.StatusCode, buf)
	}

	req, err = http.NewRequest(http.MethodPost, ui.URL+"/connector/lark/ada", strings.NewReader(`{"header":{"event_id":"e1","token":"lark-secret"},"event":{"sender":{"sender_id":{"open_id":"ou_ada"}},"message":{"message_id":"om_1","chat_id":"oc_9","content":"{\"text\":\"你好\"}"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf, _ = io.ReadAll(got.Body)
	got.Body.Close()
	if got.StatusCode != http.StatusOK || !strings.Contains(string(buf), `"accepted"`) {
		t.Fatalf("lark %d %s", got.StatusCode, buf)
	}

	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending %d", len(pending))
	}
	joined := pending[0].Content + "\n" + pending[1].Content
	if !strings.Contains(joined, "hello") || !strings.Contains(joined, "你好") || !strings.Contains(joined, "chat: 42") || !strings.Contains(joined, "chat: oc_9") {
		t.Fatal(joined)
	}
	if strings.Contains(joined, secret) || strings.Contains(joined, "lark-secret") {
		t.Fatal(joined)
	}
	if !pending[0].Untrusted || pending[0].Source != link.NameTelegram || pending[1].Source != link.NameLark {
		t.Fatalf("%+v %+v", pending[0], pending[1])
	}
	rawb, err := os.ReadFile(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	raw := string(rawb)
	if strings.Contains(raw, secret) || strings.Contains(raw, "lark-secret") {
		t.Fatal(raw)
	}
}

func TestConnectorUngranted(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root}
	ui := httptest.NewServer(s.Handler())
	defer ui.Close()
	res := postConnector(t, ui.URL+"/connector/telegram/ada", "token-value-1", `{"update_id":1,"message":{"text":"hi","chat":{"id":1}}}`)
	if res.status != http.StatusNotFound {
		t.Fatalf("%d %s", res.status, res.body)
	}
}

type posted struct {
	status int
	body   string
}

func postConnector(t *testing.T, url, secret, body string) posted {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	return posted{status: res.StatusCode, body: string(buf)}
}
