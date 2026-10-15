package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

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
