package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/work"
)

func TestEmptyProfileLeavesPrefix(t *testing.T) {
	b := testBot(t)
	file, err := b.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	composed, err := b.ComposedPersona()
	if err != nil || composed != file {
		t.Fatalf("composed %q file %q", composed, file)
	}
	msgs, _ := Assemble(Input{Persona: composed, Rules: "Stay useful.", Links: []string{link.NameTerminal}})
	if strings.Contains(msgs[0].Text(), "About:") || strings.Contains(msgs[0].Text(), "Proactivity:") {
		t.Fatal(msgs[0].Text())
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(bot.ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	composed, err = b.ComposedPersona()
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ = Assemble(Input{Persona: composed, Rules: "Stay useful.", Links: []string{link.NameTerminal}})
	if strings.Contains(msgs[0].Text(), "项目笔记") || !strings.HasPrefix(msgs[0].Text(), "### 职责\n这段写你是谁、负责什么、边界在哪；下面是正文，照着做，不要复述本说明。\n"+file) {
		t.Fatal(msgs[0].Text())
	}
}

func TestProfileEditSurfacesAsEnvUpdate(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("one")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("two")},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return now }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", t0)); err != nil {
		t.Fatal(err)
	}
	desc := "项目笔记"
	if err := b.ApplyProfile(bot.ProfileUpdate{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(5 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.reqs[1].Messages[0].Text(), "项目笔记") {
		t.Fatal(p.reqs[1].Messages[0].Text())
	}
	if !strings.Contains(p.reqs[1].Messages[0].Text(), "Be brief.") {
		t.Fatal("persona missing")
	}
	updates := envUpdates(t, b)
	last := updates[len(updates)-1]
	if !strings.Contains(last, `"profile":`) || strings.Contains(last, "persona: updated") || strings.Contains(last, "Be brief") || strings.Contains(last, "项目笔记") {
		t.Fatalf("profile diff: %s", last)
	}
}

// Snapshot only the profile and uploaded image, excluding transcript/turn state.
func profileAvatarFiles(t *testing.T, b *bot.Bot) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"config.json", "avatar", "avatar.mime"} {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(b.AvatarPath()), name))
		if errors.Is(err, os.ErrNotExist) && name != "config.json" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	return files
}

// Quiet hours are gone: a note goes out at any hour, and notes an old
// quiet window left in held.json are delivered one by one, once.
func TestNotifyAlwaysDeliversAndFlushesLegacyHeld(t *testing.T) {
	b := testBot(t)
	var desks []string
	now := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
	k := &Kernel{Bot: b, Now: func() time.Time { return now }, turn: &turn{turnSource: link.NameTimer}, Desktop: func(_ context.Context, _, body string) error {
		desks = append(desks, body)
		return nil
	}}
	out, err := (sendMessageTool{k}).Run(context.Background(), json.RawMessage(`{"text":"late","importance":"low"}`))
	if err != nil || out != "notified the owner" {
		t.Fatalf("night note %s %v", out, err)
	}
	if len(desks) != 1 || desks[0] != "late" {
		t.Fatalf("desks %v", desks)
	}
	if _, err := os.Stat(b.LegacyHeldPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("held.json written: %v", err)
	}

	if err := os.WriteFile(b.LegacyHeldPath(), []byte(`{"notes":["kept one","kept two"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	n, err := FlushLegacyHeld(context.Background(), b, now, k.Desktop)
	if err != nil || n != 2 {
		t.Fatalf("flush %d %v", n, err)
	}
	if n, err := FlushLegacyHeld(context.Background(), b, now, k.Desktop); err != nil || n != 0 {
		t.Fatalf("second flush %d %v", n, err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 || notes[0].Text != "late" || notes[1].Text != "kept one" || notes[2].Text != "kept two" {
		t.Fatalf("%+v", notes)
	}
	if len(desks) != 3 {
		t.Fatalf("desks %v", desks)
	}
}

func TestRenameUpdatesPrimaryWorkAndGroup(t *testing.T) {
	root := t.TempDir()
	ada, err := bot.Create(root, "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "Notes.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(root, "ada"); err != nil {
		t.Fatal(err)
	}
	ada, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetPinned(false); err != nil {
		t.Fatal(err)
	}
	dir := work.BotDir(root, "ada")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	bus := &peer.Bus{Root: root}
	g, err := bus.CreateGroupForOwner("u1", "ship", []string{"ada", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameBot(root, "ada", "ada2"); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Load(root, "ada"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("old name: %v", err)
	}
	next, err := bot.Load(root, "ada2")
	if err != nil {
		t.Fatal(err)
	}
	if next.Name() != "ada2" || next.Config().Pinned {
		t.Fatalf("renamed %+v", next.Config())
	}
	// The avatar seed is the old name and must survive the rename.
	if av, seed := next.AvatarState(); av != bot.AvatarGenerated || seed != "ada" {
		t.Fatalf("rename changed avatar %q %q", av, seed)
	}
	primary, err := bot.Primary(root)
	if err != nil || primary != "ada2" {
		t.Fatalf("primary %s %v", primary, err)
	}
	raw, err := os.ReadFile(filepath.Join(work.BotDir(root, "ada2"), "note.txt"))
	if err != nil || string(raw) != "keep" {
		t.Fatalf("work %q %v", raw, err)
	}
	if _, err := os.Stat(work.BotDir(root, "ada")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old work dir: %v", err)
	}
	got, err := bus.LoadGroup(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Members, ",")
	if joined != "ada2,bob" {
		t.Fatalf("members %s", joined)
	}
}

func TestRenameDomainReportsPeerFailureAfterActualMove(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "peer failure after move", true: "target conflict before peer"}[conflict], func(t *testing.T) {
			root := t.TempDir()
			if _, err := bot.Create(root, "old", "fixture", nil); err != nil {
				t.Fatal(err)
			}
			if conflict {
				if _, err := bot.Create(root, "next", "occupied", nil); err != nil {
					t.Fatal(err)
				}
			}
			// A group-list IO error follows the physical commit, and cannot
			// replace a predictable destination-conflict error.
			if err := os.WriteFile(filepath.Join(root, "groups"), []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			current, moved, err := renameBotResult(root, "old", " next ")
			if conflict {
				if !errors.Is(err, bot.ErrExists) || moved {
					t.Fatalf("conflict outcome: %v %v", moved, err)
				}
				if _, err := bot.Load(root, "old"); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !moved || current == nil || current.Name() != "next" {
				t.Fatalf("partial move: %v %v %v", current, moved, err)
			}
			if _, err := os.Stat(bot.Dir(root, "old")); !os.IsNotExist(err) {
				t.Fatalf("old route revived: %v", err)
			}
			if _, err := bot.Load(root, "next"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func transcriptHas(entries []bot.Entry, text string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), text) {
			return true
		}
	}
	return false
}

func TestProfileDomainAvatarKeepsOtherBotAndRepeat(t *testing.T) {
	for _, mode := range []string{"avatar", "description", "rename"} {
		t.Run(mode, func(t *testing.T) {
			b := testBot(t, link.NameTerminal, link.NamePersona)
			if err := b.SetAvatar([]byte("\x89PNG\r\n\x1a\nold"), "image/png"); err != nil {
				t.Fatal(err)
			}
			other, err := bot.Create(b.Root, "other", "Other bot.", nil)
			if err != nil {
				t.Fatal(err)
			}
			otherBefore := profileAvatarFiles(t, other)
			name, description, zone, seed := "renamed", "笔记助手", "UTC", "Ada#2"
			change := bot.ProfileChange{AvatarSeed: &seed}
			if mode != "avatar" {
				change.Description = &description
			}
			if mode == "rename" {
				change.Name, change.Timezone = &name, &zone
			}
			change, err = bot.ValidateProfileChange(change)
			if err != nil {
				t.Fatal(err)
			}
			k := &Kernel{Bot: b}
			receipt, err := k.ApplyProfileChange(change)
			if err != nil || !receipt.Applied || receipt.AvatarSeed != seed {
				t.Fatalf("domain receipt: %+v %v", receipt, err)
			}
			loaded, err := bot.Load(b.Root, receipt.Name)
			if err != nil {
				t.Fatal(err)
			}
			if av, got := loaded.AvatarState(); av != bot.AvatarGenerated || got != seed {
				t.Fatalf("avatar %q %q", av, got)
			}
			wantDescription := ""
			if mode != "avatar" {
				wantDescription = description
			}
			if loaded.Config().Description != wantDescription || receipt.Description != wantDescription {
				t.Fatalf("description: %+v", loaded.Config())
			}
			if mode == "rename" && (receipt.Name != name || loaded.Config().Timezone != zone || !receipt.Moved) {
				t.Fatalf("combined fields: %+v %+v", receipt, loaded.Config())
			}
			after := profileAvatarFiles(t, loaded)
			if _, ok := after["avatar"]; ok {
				t.Fatal("upload was not removed")
			}
			if _, ok := after["avatar.mime"]; ok {
				t.Fatal("upload MIME was not removed")
			}
			if _, err := k.ApplyProfileChange(change); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, profileAvatarFiles(t, k.Bot)) {
				t.Fatal("repeating the typed update changed persisted state")
			}
			if !reflect.DeepEqual(otherBefore, profileAvatarFiles(t, other)) {
				t.Fatal("another bot changed")
			}
		})
	}
}
