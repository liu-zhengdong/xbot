package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/kernel"
	"xbot/internal/mcp"
)

func TestMCP2bHumanDomainChecksOwnerAndOnlyExplicitRoot(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "owner", ForkTurns: true}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for name, owner := range map[string]string{"mine": "owner", "other": "someone-else"} {
		b, err := bot.Create(root, name, "fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{owner}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := mcp.NewManager(ctx, mcp.Options{})
	var created atomic.Int32
	s.NewResources = func(b *bot.Bot) (*kernel.BotResources, error) {
		created.Add(1)
		return kernel.NewBotResources(ctx, b, manager)
	}
	_ = muxOf(s)
	t.Cleanup(func() { s.CloseResources(); cancel(); _ = manager.Close() })
	call := func(name, header string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/bots/"+name+"/mcp/servers", nil)
		req.SetPathValue("name", name)
		if header != "" {
			req.Header.Set("X-XBot-Root", header)
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		s.mcpRuntime(rec, req)
		return rec
	}
	if rec := call("mine", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d %s", rec.Code, rec.Body)
	}
	if rec := call("other", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("other owner: %d %s", rec.Code, rec.Body)
	}
	if rec := call("mine", "/wrong/explicit/root", true); rec.Code != http.StatusConflict {
		t.Fatalf("explicit root: %d %s", rec.Code, rec.Body)
	}
	if created.Load() != 0 {
		t.Fatal("authorization/root rejection constructed resources")
	}
	if rec := call("mine", "", true); rec.Code != http.StatusOK {
		t.Fatalf("no explicit root: %d %s", rec.Code, rec.Body)
	}
	canonical, err := cliops.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if rec := call("mine", canonical, true); rec.Code != http.StatusOK {
		t.Fatalf("matching explicit root: %d %s", rec.Code, rec.Body)
	}
	if created.Load() != 1 {
		t.Fatalf("same resources not reused: %d", created.Load())
	}
}
