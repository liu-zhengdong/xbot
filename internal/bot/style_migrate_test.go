package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xbot/internal/memory"
)

func legacyStyleBot(t *testing.T) (*Bot, string) {
	t.Helper()
	b, err := Create(t.TempDir(), "ada", "Custom persona.", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.dir(), "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["style"] = map[string]string{"tone": "亲切", "length": "short", "emoji": "spare", "language": "中文"}
	raw, _ = json.Marshal(cfg)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return b, path
}

func TestLegacyStyleBecomesOneBotMemory(t *testing.T) {
	b, path := legacyStyleBot(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Load(b.Root, b.Name()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{`"tone"`, `"length"`, `"emoji"`} {
		if strings.Contains(string(raw), old) {
			t.Fatalf("legacy field survived: %s", raw)
		}
	}
	loaded, err := Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StyleCopy().Language != "中文" {
		t.Fatal(loaded.StyleCopy())
	}
	persona, _ := loaded.ReadPersona()
	if persona != "Custom persona." {
		t.Fatal(persona)
	}
	facts, err := memory.Open(b.Root).Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: b.Name()})
	if err != nil || len(facts) != 1 || facts[0].Text != "主人希望我说话：语气 亲切，篇幅 short，emoji spare。" {
		t.Fatalf("%+v %v", facts, err)
	}
	again, _ := os.ReadFile(path)
	if string(raw) != string(again) {
		t.Fatal("second load rewrote config")
	}
	// Simulate a crash after remembering, before saving the new configuration.
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	cfg["style"] = map[string]string{"tone": "亲切", "length": "short", "emoji": "spare", "language": "中文"}
	legacy, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(b.Root, b.Name()); err != nil {
		t.Fatal(err)
	}
	facts, err = memory.Open(b.Root).Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: b.Name()})
	if err != nil || len(facts) != 1 {
		t.Fatalf("retry duplicated memory: %+v %v", facts, err)
	}
}

func TestStyleMigrationFailureKeepsConfig(t *testing.T) {
	b, path := legacyStyleBot(t)
	before, _ := os.ReadFile(path)
	// A directory cannot be opened as the store's lock file.
	if err := os.MkdirAll(filepath.Join(b.dir(), "memory", ".lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(b.Root, b.Name()); err == nil {
		t.Fatal("load succeeded without saving the preference")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("lost preferences on failed migration")
	}
}

func TestLegacyTemplateStyleIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(`{"name":"ada","style":{"tone":"warm","length":"long","emoji":"often","language":"中文"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	tpl, err := ReadTemplate(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ImportTemplate(t.TempDir(), tpl, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if b.StyleCopy().Language != "中文" {
		t.Fatal(b.StyleCopy())
	}
	facts, err := memory.Open(b.Root).Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: b.Name()})
	if err != nil || len(facts) != 0 {
		t.Fatalf("template should not copy tastes: %+v %v", facts, err)
	}
}

func TestListSkipsBotWhoseStyleMigrationFails(t *testing.T) {
	b, path := legacyStyleBot(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b.dir(), "memory", ".lock"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zeta", "bob"} {
		if _, err := Create(b.Root, name, "Hello.", nil); err != nil {
			t.Fatal(err)
		}
	}
	var logs bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)
	listed, err := List(b.Root)
	if err != nil {
		t.Fatalf("one failed migration broke all bots: %v", err)
	}
	if len(listed) != 2 || listed[0].Name != "bob" || listed[1].Name != "zeta" {
		t.Fatalf("healthy bots = %+v", listed)
	}
	if !strings.Contains(logs.String(), `skip "ada"`) {
		t.Fatalf("failed bot was not logged: %s", logs.String())
	}
	if _, err := Load(b.Root, "ada"); err == nil {
		t.Fatal("direct load should still report migration failure")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("failed migration changed the old config")
	}
}
