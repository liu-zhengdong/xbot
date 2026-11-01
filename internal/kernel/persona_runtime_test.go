package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/mcp"
	"xbot/internal/personacard"
	"xbot/internal/tools"
)

func TestPersonaRuntimeSourceNoInboxDependency(t *testing.T) {
	root := t.TempDir()
	primary := runtimeBotMake(t, root, "primary", "primary duties")
	source := runtimeBotMake(t, root, "source", "old duties")
	if err := bot.SetPrimary(root, primary.Name()); err != nil {
		t.Fatal(err)
	}
	badInbox := []byte("not valid inbox JSON\n")
	if err := os.WriteFile(source.InboxPath(), badInbox, 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: source, Permissive: true}
	for _, resource := range []bool{false, true} {
		if resource {
			k.Resources = &BotResources{current: source, ctx: context.Background()}
		}
		reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: "  own duties  "})
		if err != nil || reply.Name != source.Name() || !reply.Written || reply.Effective != "next_wake" || reply.Error != "" {
			t.Fatalf("nonprimary/no owners/nil inbox resources=%v: %+v %v", resource, reply, err)
		}
	}
	if got, err := os.ReadFile(source.InboxPath()); err != nil || !bytes.Equal(got, badInbox) {
		t.Fatalf("setter touched or required Inbox: %q %v", got, err)
	}
	if got, err := primary.ReadPersona(); err != nil || got != "primary duties" {
		t.Fatalf("setter changed primary: %q %v", got, err)
	}
}

func TestPersonaRuntimeTextWriterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"small", "new duties", true},
		{"full_32KiB", strings.Repeat("a", 32<<10), true},
		{"unicode_32KiB", strings.Repeat("界", (32<<10)/3) + "ab", true},
		{"raw_over_32KiB_trimmed_valid", " \t\n" + strings.Repeat("b", 32<<10) + "\u3000\n", true},
		{"empty", "", false},
		{"unicode_whitespace", "\u3000 \t\n", false},
		{"one_byte_over", strings.Repeat("c", (32<<10)+1), false},
		{"unicode_one_byte_over", strings.Repeat("界", (32<<10)/3) + "abc", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := runtimeBotMake(t, t.TempDir(), "source", "old duties")
			before, err := os.ReadFile(b.PersonaPath())
			if err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b, Permissive: true}
			reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: tc.text})
			if reply.Written != tc.valid || (err == nil) != tc.valid || reply.Name != b.Name() {
				t.Fatalf("writer result %+v %v", reply, err)
			}
			got, readErr := os.ReadFile(b.PersonaPath())
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.valid {
				if string(got) != strings.TrimSpace(tc.text)+"\n" || reply.Effective != "next_wake" {
					t.Fatalf("writer did not preserve trim/text+NL: bytes=%d reply=%+v", len(got), reply)
				}
				info, err := os.Stat(b.PersonaPath())
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("persona mode: %v %v", info, err)
				}
			} else if !bytes.Equal(got, before) || reply.Effective != "" || reply.Error == "" {
				t.Fatalf("invalid write replaced old persona: bytes=%d reply=%+v", len(got), reply)
			}
		})
	}
}

func TestPersonaRuntimeAtomicRenameFailure(t *testing.T) {
	b := runtimeBotMake(t, t.TempDir(), "source", "old duties")
	// A nonempty directory at the destination makes the actual final Rename
	// fail, after the temporary file was written. Preserve the old file inside
	// that directory and verify the failed replacement leaves it intact.
	original, err := os.ReadFile(b.PersonaPath())
	if err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(bot.Dir(b.Root, b.Name()), "saved-persona")
	if err := os.Rename(b.PersonaPath(), saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b.PersonaPath(), 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(b.PersonaPath(), "old-persona")
	if err := os.Rename(saved, old); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Permissive: true, Redact: func(s string) string { return strings.ReplaceAll(s, b.Root, "[root]") }}
	reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: "replacement"})
	if err == nil || reply.Written || reply.Effective != "" || !strings.Contains(reply.Error, "rename") || strings.Contains(reply.Error, b.Root) {
		t.Fatalf("rename failure not truthful/redacted: %+v %v", reply, err)
	}
	if got, err := os.ReadFile(old); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("failed replacement changed old persona: %q %v", got, err)
	}
	entries, err := os.ReadDir(bot.Dir(b.Root, b.Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatal("failed atomic write left its temporary file")
		}
	}
}

func TestPersonaRuntimeCurrentRouteRename(t *testing.T) {
	k, _, _ := freshBot(t)
	b := k.Bot
	k.Permissive = true
	manager := mcp.NewManager(context.Background(), mcp.Options{})
	t.Cleanup(func() { _ = manager.Close() })
	r, err := NewBotResources(context.Background(), b, manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	k.Resources = r
	if err := r.RenameTo("renamed-source"); err != nil {
		t.Fatal(err)
	}
	reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: "current source duties"})
	if err != nil || !reply.Written || reply.Name != "renamed-source" || k.Bot != b {
		t.Fatalf("retained original source did not follow route: %+v %v", reply, err)
	}
	current, err := r.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := current.ReadPersona(); err != nil || got != "current source duties" {
		t.Fatalf("current persona %q %v", got, err)
	}
	if _, err := os.Stat(bot.Dir(b.Root, b.Name())); !os.IsNotExist(err) {
		t.Fatalf("setter recreated original directory: %v", err)
	}
}

func TestPersonaRuntimeCompositionAndUnchangedFiles(t *testing.T) {
	b := runtimeBotMake(t, t.TempDir(), "source", "config fallback duties", "u1")
	if err := b.WriteRules("custom rules"); err != nil {
		t.Fatal(err)
	}
	memory := filepath.Join(bot.Dir(b.Root, b.Name()), "memory", "memory.md")
	if err := os.MkdirAll(filepath.Dir(memory), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memory, []byte("retained memory\n"), 0600); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Permissive: true}
	// Warm the actual default-card path before taking the no-side-effect baseline.
	if _, err := k.currentComposedPersona(); err != nil {
		t.Fatal(err)
	}
	card, ok := b.PickedCard()
	if !ok {
		t.Fatal("default card not initialized")
	}
	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		err := filepath.WalkDir(b.Root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path == b.PersonaPath() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err == nil {
				out[path] = string(raw)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	next := "完整的新职责；卡片只负责口吻。"
	reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: " \n" + next + "\n"})
	if err != nil || !reply.Written {
		t.Fatalf("write %+v %v", reply, err)
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("persona setter changed config fallback/rules/memory/card or added an event/artifact")
	}
	if b.Config().Persona != "config fallback duties" {
		t.Fatal("persona setter changed config fallback")
	}
	composed, err := k.currentComposedPersona()
	if err != nil || !strings.HasPrefix(composed, next+"\n\n### 人设卡") || !strings.Contains(composed, card.Body) {
		t.Fatalf("original compositor lost duties/card: %q %v", composed, err)
	}
	env, err := k.sampleEnvironment(environment{})
	if err != nil {
		t.Fatal(err)
	}
	identity := environmentGet[string](env, "persona")
	if !strings.Contains(identity, "职责 "+shortHash(next)) || !strings.Contains(identity, card.Name+" "+shortHash(card.Body)) || strings.Contains(identity, next) {
		t.Fatalf("original duty/card fingerprint changed: %q", identity)
	}
	if _, err := os.Stat(personacard.Dir(b.Root)); err != nil {
		t.Fatal(err)
	}
}

func TestPersonaRuntimeReceiptBudgetFacts(t *testing.T) {
	for _, text := range []string{"new duties", ""} {
		b := runtimeBotMake(t, t.TempDir(), "source", "old duties")
		k := &Kernel{Bot: b, Permissive: true, Redact: func(string) string { return strings.Repeat("\x00", tools.DefaultShellOutputBytes) }}
		reply, err := k.personaBotRuntime(context.Background(), cliops.BotPersona{Text: text})
		written := text != ""
		if err == nil || reply.Written != written || (reply.Effective == "next_wake") != written || !strings.Contains(reply.Error, "response budget") {
			t.Fatalf("output failure erased actual commit facts: %+v %v", reply, err)
		}
		raw, err := json.Marshal(reply)
		limit := min(cliops.BotMaxReply, tools.DefaultShellOutputBytes)
		if err != nil || len(raw)+1 > limit || len(cliops.FormatBotPersona(reply)) > limit {
			t.Fatalf("bounded final receipt JSON=%d human=%d err=%v", len(raw)+1, len(cliops.FormatBotPersona(reply)), err)
		}
		got, err := b.ReadPersona()
		want := "old duties"
		if written {
			want = text
		}
		if err != nil || got != want {
			t.Fatalf("actual writer fact %q want %q: %v", got, want, err)
		}
	}
}
