package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/node"
	"xbot/internal/provider"
)

func TestCapsIncludeStatus(t *testing.T) {
	found := false
	for _, c := range node.Caps() {
		if c == "status" {
			found = true
		}
	}
	if !found {
		t.Fatalf("caps %v", node.Caps())
	}
}

func TestStatusCapOverCall(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	info := node.Info{ID: "abc", Name: "desk", Caps: node.Caps()}
	srv := httptest.NewServer(node.Handler("tok", info, func(req node.Request) (string, error) {
		if req.Cap != "status" {
			return "", errors.New("unknown cap " + req.Cap)
		}
		return kernel.NodeStatusJSON(root)
	}))
	defer srv.Close()
	text, err := node.Call(context.Background(), srv.URL, "tok", "status", "", false)
	if err != nil {
		t.Fatal(err)
	}
	var bots []kernel.BotStatus
	if err := json.Unmarshal([]byte(text), &bots); err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].Bot != "ada" || bots[0].State != kernel.StateIdle {
		t.Fatalf("bots %+v", bots)
	}
	if len(text) > 16<<10 {
		t.Fatalf("status %d bytes", len(text))
	}
}
