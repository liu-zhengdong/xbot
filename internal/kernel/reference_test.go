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

func TestReferenceRemindersThroughTurn(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	dir := filepath.Join(b.Root, "memory")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"root.md":  "---\nkeywords: [alpha]\n---\nROOT BODY",
		"sub/a.md": "---\nkeywords: [Alpha, '中文']\n---\nSECRET A",
		"sub/b.md": "---\nkeywords: [later]\n---\nSECRET B",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := toolCall("c1", "read_file", `{"path":"/no-such-file"}`)
	first.SetText("I will check LATER")
	p := &scripted{resps: []provider.Message{first, toolCall("c2", "read_file", `{"path":"/no-such-file"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("done")}}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.Open(b.Root), Links: []link.Link{link.NewToolsLinkAt(b.Root, "")}, Permissive: true, MaxSteps: 5}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "ALPHA 中文", time.Now())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 3 {
		t.Fatalf("requests: %d", len(p.reqs))
	}
	a, bb := filepath.Join(dir, "sub/a.md"), filepath.Join(dir, "sub/b.md")
	for i, req := range p.reqs {
		all := messagesText(req.Messages)
		if strings.Contains(all, "SECRET A") || strings.Contains(all, "SECRET B") || strings.Contains(all, filepath.Join(dir, "root.md")) {
			t.Fatalf("request %d leaked content or repeated root", i)
		}
		nudge := strings.Count(all, "Save lasting preferences")
		if nudge != 1 || strings.Count(all, a) != 1 || (i == 0 && strings.Contains(all, bb)) || (i > 0 && strings.Count(all, bb) != 1) {
			t.Fatalf("request %d: %s", i, all)
		}
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "Save lasting preferences") || strings.Contains(e.Message.Text(), "Relevant shared reference files") {
			t.Fatal("reference reminder was written into transcript")
		}
	}
	for _, m := range p.reqs[0].Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Text(), "ALPHA 中文") && (!strings.Contains(m.Text(), "\nALPHA 中文\n</untrusted_event>") || strings.Contains(m.Text(), "<reference-reminder>")) {
			t.Fatalf("owner input changed: %q", m.Text())
		}
	}
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("again")})
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "second turn", time.Now())); err != nil {
		t.Fatal(err)
	}
	replay := messagesText(p.reqs[len(p.reqs)-1].Messages)
	if strings.Count(replay, "Save lasting preferences") != 2 || strings.Count(replay, a) != 1 || strings.Count(replay, bb) != 1 {
		t.Fatalf("unexpected replay: %s", replay)
	}

}

func TestReferenceKeywordOnRootWhenIndexOmitted(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	dir := filepath.Join(b.Root, "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "root.md")
	if err := os.WriteFile(path, []byte("---\nkeywords: [needle]\n---\n"+strings.Repeat("x", 256<<10)), 0600); err != nil {
		t.Fatal(err)
	}
	got := k.referenceKeywords("NEEDLE", map[string]bool{})
	if len(got) != 1 || got[0] != path {
		t.Fatalf("root keyword with omitted index: %v", got)
	}
}

func BenchmarkReferenceKeywords(b *testing.B) {
	root := b.TempDir()
	botInstance, err := bot.Create(root, "benchmark", "brief", []string{link.NameTerminal})
	if err != nil {
		b.Fatal(err)
	}
	k := &Kernel{Bot: botInstance}
	dir := filepath.Join(root, "memory", "topics")
	if err := os.MkdirAll(dir, 0700); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.md", i)), []byte("---\nkeywords: [alpha, 中文]\n---\nDetails.\n"), 0600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k.referenceKeywords("alpha 中文", map[string]bool{})
	}
}

type referenceRetryModel struct{ requests []provider.Request }

func (p *referenceRetryModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.requests = append(p.requests, snapshotReq(req))
	if len(p.requests) == 1 {
		return provider.Message{}, provider.NewStatusError(413, "too large")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
}

func TestReferenceReminderOverflowRetry(t *testing.T) {
	b := testBot(t)
	dir := filepath.Join(b.Root, "memory", "nested")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("---\nkeywords: [needle]\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &referenceRetryModel{}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, Links: []link.Link{link.Terminal{}}, MaxSteps: 2}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "needle", time.Now())); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 2 {
		t.Fatalf("requests: %d", len(p.requests))
	}
	for i, req := range p.requests {
		all := messagesText(req.Messages)
		if strings.Count(all, "Save lasting preferences") != 1 || strings.Count(all, path) != 1 {
			t.Fatalf("retry %d: %s", i, all)
		}
	}
	notes, err := k.reminders().log.all()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, note := range notes {
		if note.Kind == KindReference {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("sidecar records: %d", n)
	}
}

func TestReferenceReminderSurvivesFork(t *testing.T) {
	k, g, box := forkKernel(t)
	dir := filepath.Join(k.Bot.Root, "memory", "nested")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("---\nkeywords: [needle]\n---\nsecret"), 0600); err != nil {
		t.Fatal(err)
	}
	ev := link.TerminalEvent("u1", "needle", time.Unix(10, 0))
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(k.Backgrounds()) != 1 {
		t.Fatal("turn did not fork")
	}
	close(g.release)
	deadline := time.Now().Add(5 * time.Second)
	for len(k.Backgrounds()) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(k.Backgrounds()) != 0 {
		t.Fatal("background did not finish")
	}
	p := k.Provider.(*scripted)
	if len(p.reqs) < 2 {
		t.Fatalf("requests: %d", len(p.reqs))
	}
	for i, req := range p.reqs {
		text := messagesText(req.Messages)
		if strings.Count(text, "Save lasting preferences") != 1 || strings.Count(text, path) != 1 {
			t.Fatalf("request %d: %s", i, text)
		}
	}
	notes, err := k.reminders().log.all()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, note := range notes {
		if note.Kind == KindReference {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("sidecar records: %d", n)
	}
}
