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

func TestReferenceEnvironmentPerBotAndSharedWorkspace(t *testing.T) {
	root := t.TempDir()
	var ks []*Kernel
	for _, name := range []string{"one", "two"} {
		b, err := bot.Create(root, name, "brief", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		ks = append(ks, &Kernel{Bot: b, Memory: memory.NopStore{}})
	}
	sample := func(k *Kernel, prev environment) environment {
		t.Helper()
		e, err := k.sampleEnvironment(prev)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b := sample(ks[0], environment{}), sample(ks[1], environment{})
	private := filepath.Join(bot.Dir(root, "one"), "memory")
	shared := filepath.Join(root, "memory")
	if info, err := os.Stat(shared); err != nil || !info.IsDir() {
		t.Fatalf("shared workspace unavailable: %v", err)
	}
	first, second := environmentGet[string](a, "reference"), environmentGet[string](b, "reference")
	if !strings.Contains(first, private) || !strings.Contains(first, shared+"\n") || !strings.Contains(second, shared+"\n") || !strings.Contains(second, filepath.Join(bot.Dir(root, "two"), "memory")) || first == second {
		t.Fatalf("private contracts: %s / %s", first, second)
	}
	if err := os.WriteFile(filepath.Join(private, "private.md"), []byte("---\ndescription: private fact\n---\nPRIVATE BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "memory.md"), []byte("---\nkeywords: [needle]\ndefaultopen: true\n---\nSYSTEM BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(private, "log"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "log", "hidden.md"), []byte("---\nkeywords: [needle]\n---\nLOG BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shared, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "common.md"), []byte("---\ndescription: shared fact\n---\nSHARED BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "open.md"), []byte("---\ndefaultopen: true\n---\nVISIBLE BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	next := sample(ks[0], a)
	other := sample(ks[1], b)
	ref := environmentGet[string](next, "reference")
	ref2 := environmentGet[string](other, "reference")
	for _, v := range []string{"private fact", "shared fact", "VISIBLE BODY", private, shared} {
		if !strings.Contains(ref, v) {
			t.Fatalf("missing %q: %s", v, ref)
		}
	}
	if strings.Index(ref, private) > strings.Index(ref, shared) {
		t.Fatal("private must precede shared")
	}
	if strings.Contains(ref2, "private fact") || !strings.Contains(ref2, "shared fact") {
		t.Fatalf("wrong scope: %s", ref2)
	}
	for _, v := range []string{"PRIVATE BODY", "SHARED BODY", "SYSTEM BODY", "memory.md", "log/"} {
		if strings.Contains(ref, v) {
			t.Fatalf("unexpected %q: %s", v, ref)
		}
	}
	if got := ks[0].referenceKeywords("needle", map[string]bool{}); len(got) != 0 {
		t.Fatalf("system keywords exposed: %v", got)
	}
	replay := replayEnvironment([]provider.Message{{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(environmentDelta(environment{}, next, "", ""))}}, "")
	if strings.Contains(environmentDelta(replay, sample(ks[0], replay), "", ""), `"reference"`) {
		t.Fatal("unchanged index republished")
	}
}

func TestReferenceBudgetsArePerSource(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	private := filepath.Join(bot.Dir(b.Root, b.Name()), "memory")
	shared := filepath.Join(b.Root, "memory")
	for _, dir := range []string{private, shared} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(private, "big.md"), []byte(strings.Repeat("x", (256<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "ok.md"), []byte("---\ndescription: shared survives\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := k.referenceText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "omitted") || !strings.Contains(text, "shared survives") {
		t.Fatalf("budget not independent: %s", text)
	}
	if err := os.Remove(filepath.Join(private, "big.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "ok.md"), []byte("---\ndescription: private survives\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "memory.md"), []byte(strings.Repeat("x", (256<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "big.md"), []byte(strings.Repeat("x", (256<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	text, err = k.referenceText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "private survives") || !strings.Contains(text, "omitted") || strings.Contains(text, "shared survives") {
		t.Fatalf("shared budget not independent: %s", text)
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
	dir := filepath.Join(bot.Dir(root, botInstance.Name()), "memory")
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
	dir := filepath.Join(bot.Dir(b.Root, b.Name()), "memory")
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
	dir := filepath.Join(bot.Dir(b.Root, b.Name()), "memory")
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
		if strings.Contains(e.Message.Text(), "Save lasting preferences") || strings.Contains(e.Message.Text(), "Relevant reference files") {
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
	dir := filepath.Join(bot.Dir(b.Root, b.Name()), "memory")
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
	dir := filepath.Join(bot.Dir(root, botInstance.Name()), "memory", "topics")
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
	dir := filepath.Join(bot.Dir(b.Root, b.Name()), "memory", "nested")
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
	dir := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "memory", "nested")
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

func TestReferenceKeywordsAcrossPrivateAndShared(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	private := filepath.Join(bot.Dir(b.Root, b.Name()), "memory")
	shared := filepath.Join(b.Root, "memory")
	for _, dir := range []string{filepath.Join(private, "nested"), filepath.Join(shared, "nested"), filepath.Join(private, "log"), filepath.Join(private, "old.bak")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(private, "nested", "a.md"), filepath.Join(shared, "nested", "b.md"), filepath.Join(private, "log", "ignored.md"), filepath.Join(private, "old.bak", "ignored.md"), filepath.Join(private, "memory.md"), filepath.Join(shared, "root.md")} {
		if err := os.WriteFile(path, []byte("---\nkeywords: [Needle, 中文]\n---\nsecret"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	matches := k.referenceKeywords("NEEDLE 中文", seen)
	if len(matches) != 2 || matches[0] != filepath.Join(private, "nested", "a.md") || matches[1] != filepath.Join(shared, "nested", "b.md") {
		t.Fatalf("matches: %v", matches)
	}
	if again := k.referenceKeywords("needle", seen); len(again) != 0 {
		t.Fatalf("duplicate: %v", again)
	}
}

func TestReferencePrivateWrittenByBot(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "private", "brief", []string{link.NameTerminal, link.NameTools})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bot.Dir(root, b.Name()), "memory", "project.md")
	args, _ := json.Marshal(map[string]string{"path": path, "content": "---\ndescription: private agreement\n---\nprivate details"})
	p := &scripted{resps: []provider.Message{toolCall("write", "write_file", string(args)), {Role: provider.RoleAssistant, Content: provider.TextContent("saved")}}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.Open(root), Links: []link.Link{link.NewToolsLinkAt(root, "")}, Permissive: true, MaxSteps: 4}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "save this", time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	first := messagesText(p.reqs[0].Messages)
	if !strings.Contains(first, "Bot reference library") || !strings.Contains(first, "shared workspace for cross-Bot facts only") || !strings.Contains(first, "Shared workspace (cross-Bot facts):") {
		t.Fatalf("wrong initial writing guidance: %s", first)
	}
	if len(p.reqs) < 2 || !strings.Contains(messagesText(p.reqs[1].Messages), "private agreement") {
		t.Fatal("private write absent from subsequent environment")
	}
	if _, err := os.Stat(filepath.Join(root, "memory")); err != nil {
		t.Fatalf("shared workspace unavailable: %v", err)
	}
	var nudge string
	for _, m := range p.reqs[0].Messages {
		if strings.Contains(m.Text(), "<reference-reminder>") {
			nudge = m.Text()
		}
	}
	if !strings.Contains(nudge, filepath.Dir(path)) || !strings.Contains(nudge, filepath.Join(root, "memory")) {
		t.Fatalf("nudge missing both paths: %s", nudge)
	}
}
