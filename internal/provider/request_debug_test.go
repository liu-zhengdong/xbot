package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestRequestDebugHotToggleAndExactBody(t *testing.T) {
	for _, protocol := range []string{ChatCompletions, AnthropicMessages, ResponsesProtocol} {
		t.Run(protocol, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			root := filepath.Join(home, ".xbot")
			marker := filepath.Join(root, "provider-debug.enabled")
			dir := filepath.Join(root, "provider-debug")
			// Deliberately put a synthetic credential in the body as well as auth.
			// The dump must preserve it, JSON escaping, and every other wire byte.
			const key = "fake-debug-key-0123456789"
			bodies := make(chan []byte, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				bodies <- raw
				w.Header().Set("Content-Type", "text/event-stream")
				switch protocol {
				case AnthropicMessages:
					io.WriteString(w, anthropicRecording)
				case ResponsesProtocol:
					io.WriteString(w, responseRecording)
				default:
					sse(w, "ok")
				}
			}))
			defer server.Close()
			p, err := New(Endpoint{Protocol: protocol, APIKey: key, BaseURL: server.URL, Model: "fake"})
			if err != nil {
				t.Fatal(err)
			}
			req := Request{
				Messages: []Message{{Role: RoleSystem, Content: TextContent("system")}, {Role: RoleUser, Content: TextContent(key + "\n\"中文 <>&\"")}},
				Tools:    []ToolDef{{Name: "inspect", Description: "inspect input", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}},
			}
			for _, enabled := range []bool{false, true, false} {
				if enabled {
					if err := os.MkdirAll(root, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(marker, nil, 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if _, err := p.Chat(context.Background(), req, nil); err != nil {
					t.Fatal(err)
				}
				wire := <-bodies
				files, err := filepath.Glob(filepath.Join(dir, "*.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !enabled {
					if len(files) != 0 {
						t.Fatal("disabled debug wrote a file")
					}
					continue
				}
				if len(files) != 1 {
					t.Fatalf("dump count = %d", len(files))
				}
				raw, err := os.ReadFile(files[0])
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, wire) || !bytes.Contains(raw, []byte(key)) {
					t.Fatal("dump differs from wire body or masks the credential")
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				messages := "messages"
				if protocol == ResponsesProtocol {
					messages = "input"
				}
				if len(body[messages]) == 0 || len(body["tools"]) == 0 || len(body["model"]) == 0 {
					t.Fatal("not a raw provider request")
				}
				for path, mode := range map[string]os.FileMode{dir: 0700, files[0]: 0600} {
					st, err := os.Stat(path)
					if err != nil || st.Mode().Perm() != mode {
						t.Fatalf("wrong permissions on %s: %v", path, err)
					}
				}
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("disabled debug created the dump directory")
			}
		})
	}
}

func TestRequestDebugConcurrentFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".xbot")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "provider-debug.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dumpRequest(ChatCompletions, fmt.Appendf(nil, `{"request":%d}`, i))
		}()
	}
	wg.Wait()
	files, err := filepath.Glob(filepath.Join(root, "provider-debug", "*.json"))
	if err != nil || len(files) != 32 {
		t.Fatalf("dump count = %d, err = %v", len(files), err)
	}
	seen := map[int]bool{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Request int }
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		seen[body.Request] = true
	}
	if len(seen) != 32 {
		t.Fatal("parallel request overwritten")
	}
}

func TestRequestDebugDisabledCost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := bytes.Repeat([]byte("x"), 1<<20)
	times := make([]time.Duration, 1000)
	for i := range times {
		start := time.Now()
		dumpRequest(ChatCompletions, raw)
		times[i] = time.Since(start)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	t.Logf("disabled check (1 MiB body, no copy/write): p50=%s p95=%s max=%s", times[500], times[950], times[999])
	entries, err := os.ReadDir(os.Getenv("HOME"))
	if err != nil || len(entries) != 0 {
		t.Fatal("disabled path created files")
	}
}

func TestRequestDebugRetryAndWriteFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".xbot")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "provider-debug.enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		sse(w, "ok")
	}))
	defer server.Close()
	c := &Client{BaseURL: server.URL, Model: "fake", APIKey: "fake", retryWait: time.Millisecond}
	req := Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hello")}}}
	if _, err := c.Chat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "provider-debug")
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != 2 {
		t.Fatalf("retry dumps = %d, err = %v", len(files), err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	// A file in place of the output directory gives a portable write failure.
	if err := os.WriteFile(dir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	msg, err := c.Chat(context.Background(), req, nil)
	if err != nil || msg.Text() != "ok" {
		t.Fatalf("debug failure broke chat: %v", err)
	}
}
