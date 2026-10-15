package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/person"
	"xbot/internal/provider"
)

func TestEvalFakeCertifies(t *testing.T) {
	root := t.TempDir()
	if err := config.Add(root, config.Provider{Name: "fake", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_FAKE_UNUSED"}, config.ModelSpec{Name: "scripted", Context: 1024}); err != nil {
		t.Fatal(err)
	}
	if err := config.Add(root, config.Provider{Name: "other", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_OTHER_UNUSED"}, config.ModelSpec{Name: "hidden", Context: 8}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--root", root, "eval", "fake/scripted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.CertifiedPath(root)); !os.IsNotExist(err) {
		t.Fatalf("fake eval wrote certified.json: %v", err)
	}
	var buf bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = run([]string{"--root", root, "provider", "list"})
	w.Close()
	os.Stdout = old
	_, _ = io.Copy(&buf, r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "fake\t") || !strings.Contains(got, "other\t") {
		t.Fatalf("providers missing\n%s", got)
	}
	if strings.Contains(got, "scripted") || strings.Contains(got, "hidden") {
		t.Fatalf("uncertified models listed\n%s", got)
	}
	buf.Reset()
	r, w, err = os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = run([]string{"--root", root, "provider", "list", "--all"})
	w.Close()
	os.Stdout = old
	_, _ = io.Copy(&buf, r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	got = buf.String()
	if !strings.Contains(got, "scripted") || !strings.Contains(got, "hidden") || !strings.Contains(got, "uncertified") {
		t.Fatalf("all %s", got)
	}
}

func TestRelativeRootSpawn(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := resolveRoot(filepath.Join(rel, "root"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("root %s", root)
	}
	model := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer model.Close()
	client := &provider.Client{BaseURL: model.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: model.Client()}
	b, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := &kernel.Kernel{
		Bot: b, Provider: client, Links: []link.Link{link.Terminal{}, link.Agents{}},
		Inbox: box, Memory: memory.Open(root), MaxSteps: 4,
		Approve: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_BRIEF", time.Now())); err != nil {
		t.Fatal(err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var joined string
	for _, e := range entries {
		joined += e.Message.Content + "\n"
	}
	if strings.Contains(joined, "dir must be absolute") || !strings.Contains(joined, "started ") {
		t.Fatalf("spawn:\n%s", joined)
	}
	agents, err := os.ReadDir(filepath.Join(bot.Dir(root, b.Name()), "agents"))
	if err != nil || len(agents) == 0 {
		t.Fatalf("agents %v %v", agents, err)
	}
}

func TestPrinterShowsAsk(t *testing.T) {
	var buf bytes.Buffer
	p := &printer{out: &buf}
	p.Tool("ask_user", "Ship it?\n1. yes\n2. no")
	if buf.String() != "Ship it?\n1. yes\n2. no\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestPrinterDropsSilent(t *testing.T) {
	for _, reply := range []string{"SILENT", "SILENT\n"} {
		var buf bytes.Buffer
		p := &printer{out: &buf}
		p.Delta(reply)
		p.Break()
		if buf.Len() != 0 {
			t.Fatalf("%q rendered %q", reply, buf.String())
		}
	}
	var buf bytes.Buffer
	p := &printer{out: &buf}
	p.Delta("hello\n")
	p.Break()
	if buf.String() != "hello\n" {
		t.Fatalf("rendered %q", buf.String())
	}
}

func TestSplitGlobal(t *testing.T) {
	root, rest, err := splitGlobal([]string{"--root", "/tmp/xbot", "chat", "ada", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if root != "/tmp/xbot" || len(rest) != 3 || rest[0] != "chat" {
		t.Fatalf("root=%q rest=%v", root, rest)
	}
	root, rest, err = splitGlobal([]string{"--root=/data", "list"})
	if err != nil || root != "/data" || rest[0] != "list" {
		t.Fatalf("root=%q rest=%v err=%v", root, rest, err)
	}
	if _, _, err := splitGlobal([]string{"--help"}); !errors.Is(err, errHelp) {
		t.Fatal(err)
	}
}

func TestParseChat(t *testing.T) {
	opt, err := parseChat([]string{"--yes", "--max-steps", "4", "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !opt.Yes || opt.MaxSteps != 4 || opt.Name != "ada" {
		t.Fatalf("%+v", opt)
	}
	opt, err = parseChat(nil)
	if err != nil || opt.Name != "" {
		t.Fatalf("empty chat %+v %v", opt, err)
	}
	opt, err = parseChat([]string{"ada", "--yes", "--max-steps", "4"})
	if err != nil || !opt.Yes || opt.MaxSteps != 4 || opt.Name != "ada" {
		t.Fatalf("%+v %v", opt, err)
	}
}

func TestParseExport(t *testing.T) {
	opt, err := parseExport([]string{"ada", "--format", "jsonl", "--tools", "--from", "2", "--to", "4", "--out", "out.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Bot != "ada" || opt.Format != "jsonl" || !opt.Tools || opt.From != 2 || opt.To != 4 || opt.Out != "out.jsonl" {
		t.Fatalf("%+v", opt)
	}
	opt, err = parseExport([]string{"--out", "a.md", "ada"})
	if err != nil || opt.Format != "md" || opt.Tools || opt.Bot != "ada" {
		t.Fatalf("%+v %v", opt, err)
	}
	if _, err := parseExport([]string{"ada", "--from", "1", "--out", "a.md"}); err == nil {
		t.Fatal("from without to")
	}
}

func TestExportCommandRoundTrip(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if err := b.Append(bot.Entry{Time: when, Message: provider.Message{Role: provider.RoleUser, Content: "seq-one"}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Time: when.Add(time.Minute), Message: provider.Message{Role: provider.RoleTool, Content: "TOOL-RESULT"}}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "ada.jsonl")
	if err := cmdExport(root, []string{"ada", "--format", "jsonl", "--tools", "--out", out}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"seq":1`) || !strings.Contains(string(raw), `"seq":2`) || !strings.Contains(string(raw), "TOOL-RESULT") {
		t.Fatalf("%s", raw)
	}
	quiet := filepath.Join(root, "quiet.jsonl")
	if err := cmdExport(root, []string{"--out", quiet, "--format", "jsonl", "ada"}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(quiet)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TOOL-RESULT") || !strings.Contains(string(raw), `"seq":1`) {
		t.Fatalf("tools flag %s", raw)
	}
}

func TestParseFork(t *testing.T) {
	opt, err := parseFork([]string{"ada", "reviewer", "--role", "Review pull requests.", "--group", "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Src != "ada" || opt.Name != "reviewer" || opt.Role != "Review pull requests." || opt.Group != "ship" {
		t.Fatalf("%+v", opt)
	}
	opt, err = parseFork([]string{"--role", "Take notes.", "ada", "neo"})
	if err != nil || opt.Src != "ada" || opt.Name != "neo" || opt.Group != "" {
		t.Fatalf("%+v %v", opt, err)
	}
	if _, err := parseFork([]string{"ada", "neo"}); err == nil {
		t.Fatal("expected role to be required")
	}
}

func TestParseNew(t *testing.T) {
	opt, err := parseNew([]string{"ada", "--persona", "Be terse."})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Name != "ada" || opt.Persona != "Be terse." {
		t.Fatalf("%+v", opt)
	}
	opt, err = parseNew([]string{"--persona", "Be terse.", "ada"})
	if err != nil || opt.Name != "ada" || opt.Persona != "Be terse." {
		t.Fatalf("%+v %v", opt, err)
	}
}

func TestMountGrants(t *testing.T) {
	b, err := bot.Create(t.TempDir(), "ada", "Be brief.", []string{"terminal", "timer", "tools", "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	links, err := mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("links %d", len(links))
	}
	if _, err := mountBot(t, []string{"nope"}); err == nil {
		t.Fatal("expected unknown link to fail")
	}
}

func TestWebhookAndWatchCLI(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "ada", "Be brief.", []string{"terminal"}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() error {
		return cmdWebhook(root, []string{"add", "ada", "github", "--public-url", "http://100.1.2.3:8787"})
	})
	if !strings.Contains(out, "url: http://100.1.2.3:8787/webhook/ada/github") || !strings.Contains(out, "secret: ") {
		t.Fatalf("add output:\n%s", out)
	}
	secret := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "secret: ") {
			secret = strings.TrimPrefix(line, "secret: ")
		}
	}
	if len(secret) < 32 {
		t.Fatalf("secret %q", secret)
	}
	b, err := bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Grants("webhook") || len(b.Config().Webhooks) != 1 {
		t.Fatalf("%+v", b.Config())
	}
	st, err := os.Stat(filepath.Join(b.WebhookDir(), "id", "github.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	listed := captureStdout(t, func() error { return cmdWebhook(root, []string{"list", "ada"}) })
	if strings.Contains(listed, secret) || !strings.Contains(listed, "github") || !strings.Contains(listed, "hmac") {
		t.Fatalf("list output:\n%s", listed)
	}
	if err := cmdWebhook(root, []string{"remove", "ada", "github"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.WebhookDir(), "id", "github.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := cmdWatch(root, []string{"add", "ada", "notes", "--path", "../etc/passwd"}); err == nil {
		t.Fatal("accepted traversal")
	}
	dir := t.TempDir()
	if err := cmdWatch(root, []string{"add", "ada", "notes", "--path", filepath.Join(dir, "*.txt")}); err != nil {
		t.Fatal(err)
	}
	b, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Grants("watch") || len(b.Config().Watches) != 1 {
		t.Fatalf("%+v", b.Config())
	}
	links, err := mount(b, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var sawHook, sawWatch bool
	for _, l := range links {
		if l.Name() == link.NameWebhook {
			sawHook = true
		}
		if l.Name() == link.NameWatch {
			sawWatch = true
		}
	}
	if !sawHook || !sawWatch {
		t.Fatal("mount missed inbound links")
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	return buf.String()
}

func TestParseProfileAndTemplate(t *testing.T) {
	opt, err := parseProfileSet([]string{"ada", "--tone", "warm", "--quiet", "22:00-07:00", "--name", "notes"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Bot != "ada" || opt.Tone != "warm" || opt.Quiet != "22:00-07:00" || opt.Name != "notes" || !opt.Set["tone"] || !opt.Set["quiet"] || !opt.Set["name"] {
		t.Fatalf("%+v", opt)
	}
	opt, err = parseProfileSet([]string{"--description", "项目笔记", "--proactivity", "quiet", "ada"})
	if err != nil || opt.Bot != "ada" || opt.Description != "项目笔记" || !opt.Set["proactivity"] || opt.Set["tone"] {
		t.Fatalf("%+v %v", opt, err)
	}
	if _, err := parseProfileSet([]string{"ada"}); err == nil {
		t.Fatal("expected a flag")
	}
	av, err := parseProfileAvatar([]string{"ada", "--file", "face.png"})
	if err != nil || av.Bot != "ada" || av.File != "face.png" || av.Clear {
		t.Fatalf("%+v %v", av, err)
	}
	av, err = parseProfileAvatar([]string{"--clear", "ada"})
	if err != nil || !av.Clear || av.File != "" {
		t.Fatalf("%+v %v", av, err)
	}
	exp, err := parseTemplateExport([]string{"ada", "--out", "ada.json"})
	if err != nil || exp.Name != "ada" || exp.Out != "ada.json" {
		t.Fatalf("%+v %v", exp, err)
	}
	imp, err := parseTemplateImport([]string{"--file", "ada.json", "--name", "ada-copy"})
	if err != nil || imp.File != "ada.json" || imp.Name != "ada-copy" {
		t.Fatalf("%+v %v", imp, err)
	}
	if _, err := parseTemplateExport([]string{"ada"}); err == nil {
		t.Fatal("expected --out")
	}
}

func TestProfileSetExportImport(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: "SECRET-TRANSCRIPT"}}); err != nil {
		t.Fatal(err)
	}
	if err := cmdProfile(root, []string{"set", "ada", "--description", "项目笔记", "--tone", "warm", "--length", "short", "--emoji", "spare", "--language", "中文", "--proactivity", "quiet", "--quiet", "22:00-07:00"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdProfile(root, []string{"set", "ada", "--name", "notes"}); err != nil {
		t.Fatal(err)
	}
	b, err = bot.Load(root, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "项目笔记" || b.StyleCopy().Tone != "warm" || b.StyleCopy().Length != "short" || b.Config().Proactivity != "quiet" || b.Config().QuietStart != "22:00" || b.Config().QuietEnd != "07:00" {
		t.Fatalf("%+v", b.Config())
	}
	out := filepath.Join(root, "notes.json")
	if err := cmdTemplate(root, []string{"export", "notes", "--out", out}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("SECRET-TRANSCRIPT")) {
		t.Fatalf("export copied the transcript: %s", raw)
	}
	if err := cmdTemplate(root, []string{"import", "--file", out, "--name", "copy"}); err != nil {
		t.Fatal(err)
	}
	copy, err := bot.Load(root, "copy")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := copy.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries %d %v", len(entries), err)
	}
	if copy.StyleCopy().Language != "中文" || !strings.Contains(mustPersona(t, copy), "Be brief.") {
		t.Fatalf("%+v", copy.Config())
	}
}

func TestLinkCLIGrantsAndRevokes(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdLink(root, []string{"install", "ada", "telegram"}); err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN is empty") {
		t.Fatalf("telegram %v", err)
	}
	if err := cmdLink(root, []string{"install", "ada", "mcp"}); err == nil || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("mcp %v", err)
	}
	if err := cmdLink(root, []string{"install", "ada", "webhook"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdLink(root, []string{"install", "ada", "coding-agent"}); err != nil {
		t.Fatal(err)
	}
	b, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	coding := false
	for _, name := range b.Config().Links {
		if name == link.NameWebhook {
			found = true
		}
		if name == link.NameCoding {
			coding = true
		}
	}
	if !found || !coding {
		t.Fatalf("links %v", b.Config().Links)
	}
	if _, err := mount(b, runnerSpec{}); err != nil {
		t.Fatal(err)
	}
	if err := cmdLink(root, []string{"remove", "ada", "webhook"}); err != nil {
		t.Fatal(err)
	}
	b, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range b.Config().Links {
		if name == link.NameWebhook {
			t.Fatal("webhook still granted")
		}
	}
	if _, err := bot.Load(root, "ada"); err != nil {
		t.Fatal(err)
	}
}

func TestGroupCLIKeepsBots(t *testing.T) {
	root := t.TempDir()
	ext, display, err := link.Account()
	if err != nil {
		t.Fatal(err)
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{who.ID}); err != nil {
		t.Fatal(err)
	}
	if err := cmdGroup(root, []string{"create", "工作"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdGroup(root, []string{"set", "ada", "工作"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdGroup(root, []string{"rename", "工作", "生活"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdGroup(root, []string{"delete", "生活"}); err != nil {
		t.Fatal(err)
	}
	b, err = bot.Load(root, "ada")
	if err != nil || b.Config().Group != "" {
		t.Fatalf("group %q %v", b.Config().Group, err)
	}
	if err := cmdGroup(root, []string{"nope"}); err == nil {
		t.Fatal("bad command was accepted")
	}
}

func mustPersona(t *testing.T, b *bot.Bot) string {
	t.Helper()
	s, err := b.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mountBot(t *testing.T, links []string) ([]link.Link, error) {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "ada", "Be brief.", links)
	if err != nil {
		t.Fatal(err)
	}
	return mount(b, runnerSpec{})
}
