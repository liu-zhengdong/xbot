package main

import (
	"errors"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
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
	if _, err := parseChat(nil); err == nil {
		t.Fatal("expected usage error")
	}
	opt, err = parseChat([]string{"ada", "--yes", "--max-steps", "4"})
	if err != nil || !opt.Yes || opt.MaxSteps != 4 || opt.Name != "ada" {
		t.Fatalf("%+v %v", opt, err)
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

func mountBot(t *testing.T, links []string) ([]link.Link, error) {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "ada", "Be brief.", links)
	if err != nil {
		t.Fatal(err)
	}
	return mount(b, runnerSpec{})
}
