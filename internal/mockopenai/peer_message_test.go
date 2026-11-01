package mockopenai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/peer"
)

func TestPeerMessageRuntimePublicMockScripts(t *testing.T) {
	for _, tc := range []struct{ cue, target, text string }{
		{"REMOTE_PING_RETRY", "bob@b", "REMOTE_OUTAGE_DELIVERY"},
		{"REMOTE_PING", "bob@b", "REMOTE_HELLO"},
		{"REMOTE_HELLO", "ada@a", "REMOTE_REPLY"},
		{"MSG_BOB", "bob", "hello from ada"},
		{"hello from ada", "ada", "reply from bob"},
		{"LOOP", "bob", "LOOP"},
	} {
		t.Run(tc.cue, func(t *testing.T) {
			name, args, content := peerMockResponse(t, []map[string]any{{"role": "user", "content": tc.cue}})
			if name != "sh" || content != "" {
				t.Fatalf("expected source shell call, got tool %q content %q", name, content)
			}
			var invocation struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal([]byte(args), &invocation); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(invocation.Command, `"$XBOT_BIN" peer-message `) || !strings.Contains(invocation.Command, shellQuote(tc.target)) || !strings.Contains(invocation.Command, " --self --text "+shellQuote(tc.text)+" --json") {
				t.Fatalf("wrong actual source caller: %s", invocation.Command)
			}
		})
	}
}

func TestPeerMessageRuntimePublicMockFactKeepsOriginalCue(t *testing.T) {
	_, args, _ := peerMockResponse(t, []map[string]any{{"role": "user", "content": "REMOTE_PING"}})
	fact, err := peer.RemoteOutgoingText(peer.RemoteOutgoingFact{Bot: "bob@b", Text: "REMOTE_HELLO", Gateway: "accepted", CallID: "actual_shell"})
	if err != nil {
		t.Fatal(err)
	}
	messages := []map[string]any{
		{"role": "user", "content": "REMOTE_PING"},
		{"role": "assistant", "tool_calls": []any{map[string]any{"id": "actual_shell", "type": "function", "function": map[string]string{"name": "sh", "arguments": args}}}},
		{"role": "user", "content": fact},
		{"role": "tool", "tool_call_id": "actual_shell", "content": `{"route":"remote","gateway":"accepted","queued":false,"recorded":true,"record_seq":3}`},
	}
	name, _, content := peerMockResponse(t, messages)
	if name != "" || content != "sent\n" {
		t.Fatalf("ordinary source fact reset original cue or peer completion: tool=%q content=%q", name, content)
	}
	// A subsequent real owner/event cue remains a new turn for the script.
	messages = append(messages, map[string]any{"role": "user", "content": "REMOTE_HELLO"})
	name, _, _ = peerMockResponse(t, messages)
	if name != "sh" {
		t.Fatalf("new real cue did not issue source shell: %q", name)
	}
}

func peerMockResponse(t *testing.T, messages []map[string]any) (name, args, content string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer fixture-only")
	w := httptest.NewRecorder()
	NewMux("").ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("mock status %d", w.Code)
	}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					Calls   []struct {
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		for _, choice := range frame.Choices {
			content += choice.Delta.Content
			for _, call := range choice.Delta.Calls {
				name, args = call.Function.Name, args+call.Function.Arguments
			}
		}
	}
	return
}
