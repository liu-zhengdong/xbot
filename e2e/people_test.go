package e2e

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/person"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/serve"
	"xbot/internal/web"
)

func TestSpawnRememberDoesNotCreateSubagentPerson(t *testing.T) {
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	root := t.TempDir()
	who, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1000"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameAgents, link.NameMemory, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{who.ID}); err != nil {
		t.Fatal(err)
	}
	k := m3Kernel(t, b, client, []link.Link{link.Terminal{}, link.Agents{}, link.Memory{}, link.NewToolsLinkAt(root, "")}, newLocal(client))
	var buf safeBuf
	k.OnDelta = func(s string) { buf.WriteString(s) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan proc.Line, 2)
	errCh := make(chan error, 1)
	go func() {
		errCh <- proc.Run(ctx, proc.Config{
			K:      k,
			Poll:   20 * time.Millisecond,
			Lines:  lines,
			Accept: terminalAccept(k, who.ID),
		})
	}()
	lines <- proc.Line{Text: "SPAWN_REMEMBER\n"}
	waitText(t, &buf, "remembered")
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not stop")
	}
	entries, err := os.ReadDir(filepath.Join(root, "people"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		if strings.HasPrefix(ent.Name(), "sa_") {
			t.Fatalf("subagent person dir %s", ent.Name())
		}
		if _, err := os.Stat(filepath.Join(root, "people", ent.Name(), "identity.json")); err != nil {
			t.Fatalf("%s is not a real person: %v", ent.Name(), err)
		}
	}
	facts, err := memory.Open(root).Recall(context.Background(), memory.Query{
		Scope: memory.ScopePerson, PersonID: who.ID, Tier: memory.TierProfile,
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range facts {
		if strings.Contains(f.Text, "owner likes water") {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner memory %+v", facts)
	}
}

func TestServeStartsWithIncompletePeopleDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "people", "garbage", "memory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "people", "garbage", "memory", ".lock"), []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	s := &web.Server{Root: root}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal}); err != nil {
		t.Fatal(err)
	}
	if err := serve.Pass(context.Background(), root, time.Now(), func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b}, nil
	}); err != nil {
		t.Fatal(err)
	}
}
