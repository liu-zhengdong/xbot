package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestModelOverlayAppendsAfterRulesAndShowsInEnv(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetBinding("deepseek", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "prompts", "models", "deepseek", "deepseek-chat.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const note = "Keep tool calls short."
	if err := os.WriteFile(path, []byte(note+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	sys := p.reqs[0].Messages[0].Text()
	rulesAt := strings.Index(sys, "Answer in the owner's language")
	noteAt := strings.Index(sys, "### 模型补丁\n仅针对当前模型的额外约束。\n"+note)
	if rulesAt < 0 || noteAt < 0 || rulesAt > noteAt {
		t.Fatalf("overlay placement\n%s", sys)
	}
	if linksAt := strings.Index(sys, "### 能力框"); linksAt >= 0 && linksAt < noteAt {
		t.Fatalf("overlay after links\n%s", sys)
	}
	updates := envUpdates(t, b)
	if len(updates) == 0 || !strings.Contains(strings.Join(updates, "\n"), `"overlay":"deepseek/deepseek-chat `+shortHash(note)) || strings.Contains(strings.Join(updates, "\n"), note) {
		t.Fatalf("env %v", updates)
	}
}

func TestUnboundBotShowsNoOverlay(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.reqs[0].Messages[0].Text(), "### 模型补丁") {
		t.Fatal(p.reqs[0].Messages[0].Text())
	}
	updates := envUpdates(t, b)
	if len(updates) == 0 || !strings.Contains(strings.Join(updates, "\n"), `"overlay":"none"`) {
		t.Fatalf("env %v", updates)
	}
}

// Exercise the provider boundary, including the snapshot after compression.
func TestOverlayShortPublicationAcrossTurnsChangesAndCompression(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetBinding("deepseek", "dedup-a"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.Root, "prompts", "models", "deepseek")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("OVERLAY_BODY_只在前缀。\n", 70)
	write := func(model, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, model+".md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("dedup-a", body)
	write("dedup-b", body)
	p := &scripted{}
	k := &Kernel{Bot: b, Provider: p, Compactor: &recCompactor{}, MaxSteps: 2, Now: func() time.Time { return time.Unix(1000, 0) }}
	k.Budget.KeepTail = 1
	wake := func(want string, publications int) {
		t.Helper()
		p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")})
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", fmt.Sprint(len(p.resps)), k.now())); err != nil {
			t.Fatal(err)
		}
		req := p.reqs[len(p.reqs)-1]
		if body != "" && (strings.Count(messagesText(req.Messages), strings.TrimSpace(body)) != 1 || !strings.Contains(req.Messages[0].Text(), "### 模型补丁\n仅针对当前模型的额外约束。\n"+strings.TrimSpace(body))) {
			t.Fatal("overlay must occur exactly once, in the fixed prefix")
		}
		n := 0
		for _, text := range envUpdates(t, b) {
			if strings.Contains(text, "OVERLAY_BODY_") || strings.Contains(text, "<model_overlay>") {
				t.Fatalf("environment repeated overlay: %s", text)
			}
			update, ok := decodeEnvironment(text)
			if ok && update.Set["overlay"] != nil {
				n++
			}
		}
		if n != publications || environmentGet[string](testPublished(t, k), "overlay") != want {
			t.Fatalf("publications=%d want=%d environment=%+v", n, publications, testPublished(t, k))
		}
	}
	want := "deepseek/dedup-a " + shortHash(strings.TrimSpace(body))
	wake(want, 1)
	prefix := p.reqs[0].Messages[0].Text()
	wake(want, 1)
	wake(want, 1)
	if p.reqs[2].Messages[0].Text() != prefix {
		t.Fatal("unchanged overlay broke prefix caching")
	}
	body += "OVERLAY_BODY_NEW_CONTENT"
	write("dedup-a", body)
	wake("deepseek/dedup-a "+shortHash(body), 2)
	write("dedup-b", body)
	if err := b.SetBinding("deepseek", "dedup-b"); err != nil {
		t.Fatal(err)
	}
	wake("deepseek/dedup-b "+shortHash(body), 3)
	n, err := b.Count()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.CompactRange(context.Background(), 1, n-1, "dedup snapshot"); err != nil {
		t.Fatal(err)
	}
	wake("deepseek/dedup-b "+shortHash(body), 4)
	wake("deepseek/dedup-b "+shortHash(body), 4)
	if err := os.Remove(filepath.Join(dir, "dedup-b.md")); err != nil {
		t.Fatal(err)
	}
	body = ""
	wake("none", 5)
	if strings.Contains(p.reqs[len(p.reqs)-1].Messages[0].Text(), "### 模型补丁") {
		t.Fatal("removed overlay remains in prefix")
	}
	t.Logf("overlay body=%d bytes; initial environment=%s", len(strings.Repeat("OVERLAY_BODY_只在前缀。\n", 70)), envUpdates(t, b)[1])
}
