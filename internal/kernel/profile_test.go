package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if !strings.Contains(msgs[0].Text(), "About: 项目笔记") || !strings.HasPrefix(msgs[0].Text(), file) {
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
	if !strings.Contains(p.reqs[1].Messages[0].Text(), "About: 项目笔记") {
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

func TestProposeProfileNeedsConfirmation(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	call := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c1", Type: "function",
		Function: provider.FunctionCall{Name: "propose_profile", Arguments: `{"description":"中文笔记"}`},
	}})}
	// A denied call cannot be retried with the same arguments. The later
	// approval uses a different proposal.
	call2 := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c2", Type: "function",
		Function: provider.FunctionCall{Name: "propose_profile", Arguments: `{"description":"中文笔记","timezone":"UTC"}`},
	}})}
	p := &scripted{resps: []provider.Message{
		call,
		{Role: provider.RoleAssistant, Content: provider.TextContent("denied-done")},
		call2,
		{Role: provider.RoleAssistant, Content: provider.TextContent("saved")},
		{Role: provider.RoleAssistant, Content: provider.TextContent("next")},
	}}
	approved := false
	k := &Kernel{
		Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 4,
		Now: func() time.Time { return now },
		Approve: func(_ context.Context, name, summary string) (bool, error) {
			if name != "propose_profile" || !strings.Contains(summary, "中文") {
				t.Fatalf("confirm %s %s", name, summary)
			}
			return approved, nil
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change it", t0)); err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "" || b.Config().Proactivity != "" {
		t.Fatalf("denied profile applied: %+v", b.Config())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !transcriptHas(entries, "denied by user") {
		t.Fatal("missing denial")
	}
	approved = true
	now = t0.Add(5 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change it again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "中文笔记" || b.Config().Timezone != "UTC" || b.Config().Proactivity != "" {
		t.Fatalf("profile %+v", b.Config())
	}
	now = t0.Add(10 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "third", t0.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	prefix := p.reqs[len(p.reqs)-1].Messages[0].Text()
	if !strings.Contains(prefix, "About: 中文笔记") || strings.Contains(prefix, "Proactivity:") {
		t.Fatal(prefix)
	}
	updates := envUpdates(t, b)
	last := strings.Join(updates, "\n")
	if !strings.Contains(last, `"profile":`) || strings.Contains(last, "中文") {
		t.Fatalf("env %s", last)
	}
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
	out, err := (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"late","importance":"low"}`))
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

func transcriptHas(entries []bot.Entry, text string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), text) {
			return true
		}
	}
	return false
}

func TestProfileRejectsRemovedStyleFields(t *testing.T) {
	for _, field := range []string{"tone", "length", "emoji", "language", "quiet_start", "quiet_end"} {
		if _, _, _, err := parseProfileArgs(json.RawMessage(`{"` + field + `":"old"}`)); err == nil {
			t.Fatalf("removed field %s accepted", field)
		}
	}
}
