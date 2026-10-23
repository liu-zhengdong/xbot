package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sse writes one content chunk and the terminator.
func sse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"`+text+`"}}]}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// stallRelease is closed before httptest.Server.Close, so a handler parked in a
// select (mimicking a server that went quiet) is let go and Close can finish.
// Register `defer srv.Close()` first, then `defer close(release)`.
func newStall() (chan struct{}, func(func()) func(http.ResponseWriter, *http.Request)) {
	release := make(chan struct{})
	handler := func(extra func()) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			if extra != nil {
				extra()
			}
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
	}
	return release, handler
}

func TestChatRetriesWholeRequestTimeout(t *testing.T) {
	var n atomic.Int32
	release, hang := newStall()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			hang(nil)(w, r)
			return
		}
		sse(w, "pong")
	}))
	defer srv.Close()
	defer close(release)

	c := &Client{
		BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(),
		retryWait: time.Millisecond, RequestTimeout: 200 * time.Millisecond,
	}
	var deltas []string
	msg, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "pong" || strings.Join(deltas, "") != "pong" {
		t.Fatalf("msg %q deltas %v", msg.Text(), deltas)
	}
	if n.Load() != 2 {
		t.Fatalf("attempts %d, want the hung request retried once", n.Load())
	}
}

func TestChatRequestTimeoutIsTheFinalError(t *testing.T) {
	var n atomic.Int32
	release, hang := newStall()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		hang(nil)(w, r)
	}))
	defer srv.Close()
	defer close(release)

	c := &Client{
		BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(),
		retryWait: time.Millisecond, RequestTimeout: 50 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRequestTimeout) {
			t.Fatalf("err %v; want a request-timeout error", err)
		}
		if !retryable(err) {
			t.Fatalf("a request timeout must be retryable: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chat did not return; the request timeout is not enforced")
	}
	if n.Load() != 3 {
		t.Fatalf("attempts %d, want 3 (two retries)", n.Load())
	}
}

func TestChatRetriesStreamIdleTimeout(t *testing.T) {
	var n atomic.Int32
	release, hang := newStall()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			// Headers, then silence: nothing has reached the owner yet.
			hang(func() {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
			})(w, r)
			return
		}
		sse(w, "pong")
	}))
	defer srv.Close()
	defer close(release)

	c := &Client{
		BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(),
		retryWait: time.Millisecond, StreamIdleTimeout: 200 * time.Millisecond,
	}
	var deltas []string
	msg, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "pong" || strings.Join(deltas, "") != "pong" {
		t.Fatalf("content %q deltas %q", msg.Text(), deltas)
	}
	if n.Load() != 2 {
		t.Fatalf("attempts %d, want 2", n.Load())
	}
}

// Text that already reached the owner cannot be taken back, so a stream that
// stalls after it is not retried: a second attempt would repeat it.
func TestChatDoesNotRetryAfterTextWentOut(t *testing.T) {
	var n atomic.Int32
	release, hang := newStall()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		hang(func() {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"par"}}]}`+"\n\n")
			w.(http.Flusher).Flush()
		})(w, r)
	}))
	defer srv.Close()
	defer close(release)

	c := &Client{
		BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(),
		retryWait: time.Millisecond, StreamIdleTimeout: 200 * time.Millisecond,
	}
	var deltas []string
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, func(s string) {
		deltas = append(deltas, s)
	})
	if !errors.Is(err, ErrStreamIdleTimeout) {
		t.Fatalf("err %v", err)
	}
	if got := strings.Join(deltas, ""); got != "par" || n.Load() != 1 {
		t.Fatalf("deltas %q attempts %d", got, n.Load())
	}
}

// The first words go out while the model is still writing the rest.
func TestChatStreamsBeforeTheAnswerEnds(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"first"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":" rest"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(), retryWait: time.Millisecond}
	got := make(chan string, 4)
	done := make(chan Message, 1)
	go func() {
		msg, _ := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, func(s string) { got <- s })
		done <- msg
	}()
	select {
	case s := <-got:
		if s != "first" {
			t.Fatalf("first delta %q", s)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("no delta before the answer ended")
	}
	close(release)
	if msg := <-done; msg.Text() != "first rest" {
		t.Fatalf("content %q", msg.Text())
	}
}

func TestStreamIdleTimeoutIsOffWhenNegative(t *testing.T) {
	c := &Client{StreamIdleTimeout: -1}
	if c.streamIdleTimeout() > 0 {
		t.Fatalf("idle timeout %v; want the check off", c.streamIdleTimeout())
	}
	c = &Client{}
	if c.streamIdleTimeout() != defaultStreamIdleTimeout || c.requestTimeout() != defaultRequestTimeout {
		t.Fatalf("defaults %v / %v", c.streamIdleTimeout(), c.requestTimeout())
	}
	if defaultRequestTimeout != 3*time.Minute || defaultStreamIdleTimeout != 90*time.Second {
		t.Fatalf("defaults changed: %v / %v", defaultRequestTimeout, defaultStreamIdleTimeout)
	}
}
