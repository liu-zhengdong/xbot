package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type opencodeTransport func(*http.Request) (*http.Response, error)

func (f opencodeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenCodeGoSession(t *testing.T) {
	for _, base := range []string{"https://opencode.ai/zen/go/v1/", "https://opencode.ai/inference/go/openai/v1/"} {
		t.Run(base, func(t *testing.T) { testOpenCodeGoSession(t, base) })
	}
}

func testOpenCodeGoSession(t *testing.T, base string) {
	var session string
	calls := 0
	p, err := New(Endpoint{Protocol: ChatCompletions, BaseURL: base, APIKey: "test", Model: "mock", HTTPClient: &http.Client{Transport: opencodeTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		id := r.Header.Get("x-opencode-session")
		if _, err := uuid.Parse(id); err != nil {
			return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"type":"MissingSessionID","message":"Request is missing x-opencode-session"}}`))}, nil
		}
		if session == "" {
			session = id
		} else if session != id {
			t.Errorf("session changed: %q -> %q", session, id)
		}
		if r.URL.String() != strings.TrimRight(base, "/")+"/chat/completions" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		status, body := 200, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"
		if calls == 1 {
			status, body = 502, "try again"
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	p.(*Client).retryWait = time.Millisecond
	request := Request{Messages: []Message{{Role: RoleUser, Content: TextContent("ping")}}}
	for range 2 {
		msg, err := p.Chat(context.Background(), request, nil)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Text() != "pong" {
			t.Fatalf("reply = %q", msg.Text())
		}
		request.Messages = append(request.Messages, msg, Message{Role: RoleUser, Content: TextContent("again")})
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want retry and follow-up", calls)
	}
	other, err := New(Endpoint{Protocol: ChatCompletions, BaseURL: "https://opencode.ai/zen/go/v1", APIKey: "test", Model: "mock", HTTPClient: &http.Client{Transport: opencodeTransport(func(r *http.Request) (*http.Response, error) {
		id := r.Header.Get("x-opencode-session")
		if _, err := uuid.Parse(id); err != nil || id == session {
			t.Errorf("separate client session = %q", id)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Chat(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeGoSessionScope(t *testing.T) {
	for _, base := range []string{"https://api.deepseek.com/v1", "https://opencode.ai/zen/v1", "https://opencode.ai.example/zen/go/v1", "https://example.com/zen/go/v1"} {
		t.Run(base, func(t *testing.T) {
			p, err := New(Endpoint{Protocol: ChatCompletions, BaseURL: base, APIKey: "test", Model: "mock", HTTPClient: &http.Client{Transport: opencodeTransport(func(r *http.Request) (*http.Response, error) {
				if id := r.Header.Get("x-opencode-session"); id != "" {
					t.Errorf("unexpected session header %q for %s", id, base)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Chat(context.Background(), Request{}, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
