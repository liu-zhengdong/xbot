package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestSharedReferenceEnvironment(t *testing.T) {
	root := t.TempDir()
	var kernels []*Kernel
	for _, name := range []string{"one", "two"} {
		b, err := bot.Create(root, name, "brief", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		kernels = append(kernels, &Kernel{Bot: b, Memory: memory.NopStore{}})
	}
	sample := func(k *Kernel, prev environment) environment {
		t.Helper()
		e, err := k.sampleEnvironment(prev)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a := sample(kernels[0], environment{})
	b := sample(kernels[1], environment{})
	path := filepath.Join(root, "memory")
	empty := environmentGet[string](a, "reference")
	if empty != environmentGet[string](b, "reference") || !strings.Contains(empty, path) {
		t.Fatalf("different or missing contract: %q", empty)
	}
	if err := os.WriteFile(filepath.Join(path, "topic.md"), []byte("---\ndescription: project facts\n---\nSECRET BODY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "open.md"), []byte("---\ndefaultopen: true\n---\nVISIBLE BODY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "sub", "deep.md"), []byte("DEEP BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	next := sample(kernels[0], a)
	ref := environmentGet[string](next, "reference")
	for _, want := range []string{"topic.md: project facts", "VISIBLE BODY", "sub/"} {
		if !strings.Contains(ref, want) {
			t.Fatalf("missing %q: %s", want, ref)
		}
	}
	for _, unwanted := range []string{"SECRET BODY", "DEEP BODY"} {
		if strings.Contains(ref, unwanted) {
			t.Fatalf("unexpected body: %s", ref)
		}
	}
	delta := environmentDelta(a, next, "", "")
	if !strings.Contains(delta, "VISIBLE BODY") {
		t.Fatal("changed index not published")
	}
	replayed := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(environmentDelta(environment{}, next, "", ""))}}, "")
	again := sample(kernels[0], replayed)
	if strings.Contains(environmentDelta(replayed, again, "", ""), `"reference"`) {
		t.Fatal("unchanged index republished")
	}
	other := sample(kernels[1], b)
	if environmentGet[string](other, "reference") != ref {
		t.Fatal("second bot did not see shared update")
	}
}

func TestReferenceWrittenByOneBotAppearsForAnother(t *testing.T) {
	root := t.TempDir()
	first, err := bot.Create(root, "first", "brief", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	second, err := bot.Create(root, "second", "brief", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "memory", "agreement.md")
	args, err := json.Marshal(map[string]string{"path": path, "content": "---\ndescription: deployment agreement\n---\nUse blue green.\n"})
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{toolCall("write", "write_file", string(args)), {Role: provider.RoleAssistant, Content: provider.TextContent("saved")}}}
	k := &Kernel{Bot: first, Provider: p, Memory: memory.Open(root), Links: []link.Link{link.NewToolsLinkAt(root, "")}, Permissive: true, MaxSteps: 4}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "save the agreement", time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	p2 := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
	k2 := &Kernel{Bot: second, Provider: p2, Memory: memory.Open(root), Links: []link.Link{link.NewToolsLinkAt(root, "")}, Permissive: true, MaxSteps: 4}
	if err := k2.Handle(context.Background(), link.TerminalEvent("u1", "what agreements?", time.Now())); err != nil {
		t.Fatal(err)
	}
	got := messagesText(p2.reqs[0].Messages)
	if !strings.Contains(got, "deployment agreement") || !strings.Contains(got, filepath.Dir(path)) || strings.Contains(got, "Use blue green.") {
		t.Fatalf("unexpected second bot context: %s", got)
	}
}

func BenchmarkReferenceIndex(b *testing.B) {
	root := b.TempDir()
	botInstance, err := bot.Create(root, "benchmark", "brief", []string{link.NameTerminal})
	if err != nil {
		b.Fatal(err)
	}
	k := &Kernel{Bot: botInstance}
	dir := filepath.Join(root, "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.md", i)), []byte("---\ndescription: project notes\n---\nDetails.\n"), 0600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := k.referenceText(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReferenceOverBudgetOmitsWholeIndex(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	dir := filepath.Join(b.Root, "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "small.md"), []byte("---\ndescription: MUST NOT APPEAR\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.md"), []byte(strings.Repeat("x", 256<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := k.referenceText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, dir) || !strings.Contains(text, "omitted") || strings.Contains(text, "MUST NOT APPEAR") {
		t.Fatalf("unexpected over-budget rendering: %s", text)
	}
}
