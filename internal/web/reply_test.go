package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

type replyProvider struct {
	content string
	seen    string
}

func (p *replyProvider) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	for _, m := range req.Messages {
		p.seen += m.Content + "\n"
	}
	if onDelta != nil && p.content != "" {
		onDelta(p.content)
	}
	return provider.Message{Role: provider.RoleAssistant, Content: p.content}, nil
}

func TestReplyToRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{
		Time:    time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleAssistant, Content: "EARLIER-ANSWER"},
	}); err != nil {
		t.Fatal(err)
	}
	model := &replyProvider{content: "reply_to: 3\nback to you"}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: model, Memory: memory.NopStore{}, MaxSteps: 2}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	bad := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"nope","reply_to":99}`)
	if bad.StatusCode != http.StatusBadRequest || !bytes.Contains(bad.Body, []byte("bad reply_to")) {
		t.Fatalf("invalid seq %d %s", bad.StatusCode, bad.Body)
	}
	zero := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"nope","reply_to":0}`)
	if zero.StatusCode != http.StatusBadRequest {
		t.Fatalf("zero seq %d %s", zero.StatusCode, zero.Body)
	}

	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"and this","reply_to":1}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("send %d %s", res.StatusCode, res.Body)
	}
	ev := parseSSE(t, res.Body)
	done, ok := ev["done"]
	if !ok {
		t.Fatalf("events %+v", ev)
	}
	var body doneBody
	if err := json.Unmarshal([]byte(done), &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != "back to you" || body.ReplyTo != 3 || strings.Contains(done, "reply_to: 3") {
		t.Fatalf("done %+v raw %s", body, done)
	}
	if !strings.Contains(model.seen, "user is replying to your earlier message (seq 1): EARLIER-ANSWER") {
		t.Fatalf("turn input\n%s", model.seen)
	}

	tr := getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie)
	if !bytes.Contains(tr, []byte(`"reply_to":1`)) || !bytes.Contains(tr, []byte(`"reply_to":3`)) {
		t.Fatalf("transcript %s", tr)
	}
	if !bytes.Contains(tr, []byte(`"content":"and this"`)) || bytes.Contains(tr, []byte("user is replying to your earlier message")) {
		t.Fatalf("stored quote leaked %s", tr)
	}

	md := getAuth(t, ui.URL+"/api/bots/ada/export?format=md", cookie)
	js := getAuth(t, ui.URL+"/api/bots/ada/export?format=jsonl", cookie)
	if !bytes.Contains(md, []byte("reply_to: 1")) || !bytes.Contains(md, []byte("reply_to: 3")) {
		t.Fatalf("md\n%s", md)
	}
	if !bytes.Contains(js, []byte(`"reply_to":1`)) || !bytes.Contains(js, []byte(`"reply_to":3`)) {
		t.Fatalf("jsonl\n%s", js)
	}
}

func TestEmptyTranscriptRejectsReplyTo(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Create(root, "ada", "persona", []string{link.NameWeb}); err != nil {
		t.Fatal(err)
	}
	b, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"hi","reply_to":1}`)
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(res.Body, []byte("bad reply_to")) {
		t.Fatalf("status %d %s", res.StatusCode, res.Body)
	}
}

type httpResult struct {
	StatusCode int
	Body       []byte
}

func postJSON(t *testing.T, url string, cookie *http.Cookie, body string) httpResult {
	t.Helper()
	return sendJSON(t, http.MethodPost, url, cookie, body)
}

func patchJSON(t *testing.T, url string, cookie *http.Cookie, body string) httpResult {
	t.Helper()
	return sendJSON(t, http.MethodPatch, url, cookie, body)
}

func sendJSON(t *testing.T, method, url string, cookie *http.Cookie, body string) httpResult {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return httpResult{StatusCode: res.StatusCode, Body: raw}
}

func getAuth(t *testing.T, url string, cookie *http.Cookie) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get %s %d %s", url, res.StatusCode, raw)
	}
	return raw
}

func parseSSE(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var name, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && name != "":
			out[name] = data
			name, data = "", ""
		}
	}
	return out
}
