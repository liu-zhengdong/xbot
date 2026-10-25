package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithoutHostedSearchSuppressesAdapterDefaults(t *testing.T) {
	for _, protocol := range []string{ResponsesProtocol, AnthropicMessages} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var in struct{ Tools []struct{ Type, Name string } }
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				for _, tool := range in.Tools {
					if tool.Type == "web_search" || tool.Type == "web_search_20250305" {
						t.Error("hosted search escaped suppression")
					}
				}
				if len(in.Tools) != 1 {
					t.Errorf("local search lost: %+v", in.Tools)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if protocol == ResponsesProtocol {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n")
				} else {
					fmt.Fprint(w, anthropicRecording)
				}
			}))
			defer s.Close()
			original, err := New(Endpoint{Protocol: protocol, BaseURL: s.URL, Model: "fake", APIKey: "fake", HostedSearch: true})
			if err != nil {
				t.Fatal(err)
			}
			p := WithoutHostedSearch(original)
			if HasHostedSearch(p) || !HasHostedSearch(original) {
				t.Fatal("capability mutated")
			}
			ep, ok := EndpointOf(p)
			if !ok || ep.HostedSearch || ep.Protocol != protocol {
				t.Fatal(ep)
			}
			_, err = p.Chat(context.Background(), Request{HostedSearch: true, Tools: []ToolDef{{Name: "web_search", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}}, nil)
			if err != nil || calls != 1 {
				t.Fatalf("%v calls=%d", err, calls)
			}
		})
	}
}
