//go:build smoke

package smoke

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/person"
)

func TestSenderNameBinary(t *testing.T) {
	requests := make(chan stubRequest, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req stubRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		select {
		case requests <- req:
		default:
		}
		streamOwner(w, req, "收到")
	}))
	t.Cleanup(model.Close)
	srv := Start(t, &Stub{srv: model})
	ext, _, err := link.Account()
	if err != nil {
		t.Fatal(err)
	}
	who, err := person.Identify(srv.Root, person.Account{Link: link.NameTerminal, ExternalID: ext}, "刘政东")
	if err != nil {
		t.Fatal(err)
	}
	// Start already identified the OS owner with its account name.
	who.DisplayName = "刘政东"
	if err := person.Record(srv.Root, "", "", time.Time{}, who); err != nil {
		t.Fatal(err)
	}
	srv.CreateBot("ada", "Be brief.")
	reply := srv.Send("ada", "你好")
	if len(reply.Delivered) != 1 || reply.Delivered[0].Text != "收到" {
		t.Fatalf("owner reply was not delivered: %+v", reply)
	}

	var source struct {
		Sender   string `json:"sender"`
		SenderID string `json:"sender_id"`
	}
	select {
	case req := <-requests:
		for _, msg := range req.Messages {
			_, rest, ok := strings.Cut(msg.Content, "<environment_update>")
			// Time announcements share this frame but carry plain text.
			if !ok || !strings.HasPrefix(strings.TrimSpace(rest), "{") {
				continue
			}
			body, _, _ := strings.Cut(rest, "</environment_update>")
			var env struct {
				Set map[string]json.RawMessage `json:"set"`
			}
			if err := json.Unmarshal([]byte(body), &env); err != nil {
				t.Fatal(err)
			}
			if raw, ok := env.Set["source"]; ok {
				if err := json.Unmarshal(raw, &source); err != nil {
					t.Fatal(err)
				}
			}
		}
	default:
		t.Fatal("model received no request")
	}
	if source.Sender != "刘政东" || source.SenderID != who.ID {
		t.Fatalf("latest environment source = %+v, want sender=刘政东 sender_id=%s", source, who.ID)
	}
}
