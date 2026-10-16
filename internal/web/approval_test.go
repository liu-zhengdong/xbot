package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/mockopenai"
	"xbot/internal/provider"
)

func TestWebApprovalAllowAndDeny(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	model := httptest.NewServer(mockopenai.NewMux(work))
	defer model.Close()
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{
			Bot: b, Provider: client, MaxSteps: 4, Budget: compact.DefaultBudget(),
			Links:   []link.Link{link.Web{}, link.NewToolsLinkAt(work, "")},
			WorkDir: work,
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	allow := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	ev := waitEvent(t, allow, "approval")
	var card struct {
		ID      string `json:"id"`
		Tool    string `json:"tool"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	if card.ID == "" || card.Tool != "shell" || !strings.Contains(card.Summary, "mkdir") {
		t.Fatalf("card %+v", card)
	}
	decide(t, ui.URL, cookie, card.ID, true)
	sawRun := false
	for ev := range allow {
		if ev.name == "result" && strings.Contains(ev.data, "exit 0") {
			sawRun = true
		}
		if ev.name == "error" {
			t.Fatalf("error event %s", ev.data)
		}
	}
	if !sawRun {
		t.Fatal("approved tool did not run")
	}
	if _, err := os.Stat(filepath.Join(work, "approved-dir")); err != nil {
		t.Fatal(err)
	}
	if states := approvalStates(t, ui.URL, cookie, card.ID); !reflect.DeepEqual(states, []string{bot.ApprovalPending, bot.ApprovalApproved}) {
		t.Fatalf("allow states %v", states)
	}

	deny := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	ev = waitEvent(t, deny, "approval")
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	decide(t, ui.URL, cookie, card.ID, false)
	sawDeny := false
	for ev := range deny {
		if ev.name == "result" && strings.Contains(ev.data, "denied by user") {
			sawDeny = true
		}
		if ev.name == "error" {
			t.Fatalf("error event %s", ev.data)
		}
	}
	if !sawDeny {
		t.Fatal("deny did not report denied by user")
	}
	if states := approvalStates(t, ui.URL, cookie, card.ID); !reflect.DeepEqual(states, []string{bot.ApprovalPending, bot.ApprovalDeclined}) {
		t.Fatalf("deny states %v", states)
	}
}

// approvalStates reads the transcript and returns the persisted card states for
// one approval id, oldest first. An empty slice means the card never persisted.
func approvalStates(t *testing.T, base string, cookie *http.Cookie, id string) []string {
	t.Helper()
	raw := getAuth(t, base+"/api/bots/ada/transcript", cookie)
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var states []string
	for _, m := range payload.Messages {
		card, ok := bot.ParseApprovalCard(m.Content)
		if ok && card.ID == id {
			if m.Role != "user" {
				t.Fatalf("card stored with role %q", m.Role)
			}
			states = append(states, card.State)
		}
	}
	return states
}

func TestWebApprovalTimeoutRecordsDeclined(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	model := httptest.NewServer(mockopenai.NewMux(work))
	defer model.Close()
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	s := &Server{Root: root, Owner: "u1", ApprovalWait: 300 * time.Millisecond}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{
			Bot: b, Provider: client, MaxSteps: 4, Budget: compact.DefaultBudget(),
			Links:   []link.Link{link.Web{}, link.NewToolsLinkAt(work, "")},
			WorkDir: work,
		}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	stream := postSSE(t, ui.URL, cookie, "NEED_APPROVAL")
	ev := waitEvent(t, stream, "approval")
	var card struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(ev.data), &card); err != nil {
		t.Fatal(err)
	}
	for ev := range stream {
		if ev.name == "error" {
			t.Fatalf("error event %s", ev.data)
		}
	}
	if states := approvalStates(t, ui.URL, cookie, card.ID); !reflect.DeepEqual(states, []string{bot.ApprovalPending, bot.ApprovalDeclined}) {
		t.Fatalf("timeout states %v", states)
	}
}

type sseEvent struct {
	name string
	data string
}

func loginCookie(t *testing.T, base, tok string) *http.Cookie {
	t.Helper()
	res, err := http.Post(base+"/api/login", "application/json", strings.NewReader(`{"token":"`+tok+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("login %d", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("no cookie")
	return nil
}

func postSSE(t *testing.T, base string, cookie *http.Cookie, text string) <-chan sseEvent {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/api/bots/ada/messages", bytes.NewBufferString(`{"text":"`+text+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("message %d %s", res.StatusCode, raw)
	}
	ch := make(chan sseEvent, 32)
	go func() {
		defer close(ch)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		var name, data string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				ch <- sseEvent{name: name, data: data}
				name, data = "", ""
			}
		}
	}()
	return ch
}

func waitEvent(t *testing.T, ch <-chan sseEvent, name string) sseEvent {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream ended before %s", name)
			}
			if ev.name == name {
				return ev
			}
			if ev.name == "error" {
				t.Fatalf("error event %s", ev.data)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", name)
		}
	}
}

func decide(t *testing.T, base string, cookie *http.Cookie, id string, allow bool) {
	t.Helper()
	body := `{"allow":false}`
	if allow {
		body = `{"allow":true}`
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/bots/ada/approvals/"+id, strings.NewReader(body))
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
	if res.StatusCode != 200 {
		t.Fatalf("decide %d %s", res.StatusCode, raw)
	}
}
