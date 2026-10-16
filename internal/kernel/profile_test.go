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
	if strings.Contains(msgs[0].Content, "Tone:") || strings.Contains(msgs[0].Content, "Proactivity:") {
		t.Fatal(msgs[0].Content)
	}
	tone := "warm and plain"
	if err := b.ApplyProfile(bot.ProfileUpdate{Tone: &tone}); err != nil {
		t.Fatal(err)
	}
	composed, err = b.ComposedPersona()
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ = Assemble(Input{Persona: composed, Rules: "Stay useful.", Links: []string{link.NameTerminal}})
	if !strings.Contains(msgs[0].Content, "Tone: warm and plain") || !strings.HasPrefix(msgs[0].Content, file) {
		t.Fatal(msgs[0].Content)
	}
}

func TestProfileEditSurfacesAsEnvUpdate(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2, Now: func() time.Time { return now }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", t0)); err != nil {
		t.Fatal(err)
	}
	tone := "warm and plain"
	if err := b.ApplyProfile(bot.ProfileUpdate{Tone: &tone}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(5 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.reqs[1].Messages[0].Content, "Tone: warm and plain") {
		t.Fatal(p.reqs[1].Messages[0].Content)
	}
	if !strings.Contains(p.reqs[1].Messages[0].Content, "Be brief.") {
		t.Fatal("persona missing")
	}
	updates := envUpdates(t, b)
	last := updates[len(updates)-1]
	if !strings.Contains(last, "profile: updated") || strings.Contains(last, "persona: updated") || strings.Contains(last, "Be brief") || strings.Contains(last, "warm") {
		t.Fatalf("profile diff: %s", last)
	}
}

func TestProposeProfileNeedsConfirmation(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona)
	t0 := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := t0
	call := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "c1", Type: "function",
		Function: provider.FunctionCall{Name: "propose_profile", Arguments: `{"tone":"warm and plain"}`},
	}}}
	p := &scripted{resps: []provider.Message{
		call,
		{Role: provider.RoleAssistant, Content: "denied-done"},
		call,
		{Role: provider.RoleAssistant, Content: "saved"},
		{Role: provider.RoleAssistant, Content: "next"},
	}}
	approved := false
	k := &Kernel{
		Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 4,
		Now: func() time.Time { return now },
		Approve: func(_ context.Context, name, summary string) (bool, error) {
			if name != "propose_profile" || !strings.Contains(summary, "warm and plain") {
				t.Fatalf("confirm %s %s", name, summary)
			}
			return approved, nil
		},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "change it", t0)); err != nil {
		t.Fatal(err)
	}
	if b.StyleCopy().Tone != "" || b.Config().Proactivity != "" {
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
	if b.StyleCopy().Tone != "warm and plain" || b.Config().Proactivity != "" {
		t.Fatalf("profile %+v %s", b.StyleCopy(), b.Config().Proactivity)
	}
	now = t0.Add(10 * time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "third", t0.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	prefix := p.reqs[len(p.reqs)-1].Messages[0].Content
	if !strings.Contains(prefix, "Tone: warm and plain") || strings.Contains(prefix, "Proactivity:") {
		t.Fatal(prefix)
	}
	updates := envUpdates(t, b)
	last := updates[len(updates)-1]
	if !strings.Contains(last, "profile: updated") || strings.Contains(last, "warm and plain") {
		t.Fatalf("env %s", last)
	}
}

func TestNotifyThresholdAndQuietHours(t *testing.T) {
	b := testBot(t)
	start, end, utc := "22:00", "07:00", "UTC"
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &start, QuietEnd: &end, Timezone: &utc}); err != nil {
		t.Fatal(err)
	}
	var desks []string
	now := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
	k := &Kernel{Bot: b, Now: func() time.Time { return now }, turn: &turn{turnSource: link.NameTimer}, Desktop: func(_ context.Context, _, body string) error {
		desks = append(desks, body)
		return nil
	}}
	out, err := (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"ordinary","importance":"low"}`))
	if err != nil || out != "held until quiet hours end" {
		t.Fatalf("routine low %s %v", out, err)
	}
	held, err := os.ReadFile(b.HeldPath())
	if err != nil || !strings.Contains(string(held), "ordinary") {
		t.Fatalf("held %s %v", held, err)
	}
	out, err = (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"urgent","importance":"high"}`))
	if err != nil || out != "held until quiet hours end" {
		t.Fatalf("%s %v", out, err)
	}
	out, err = (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"also urgent","importance":"high"}`))
	if err != nil || out != "held until quiet hours end" {
		t.Fatalf("%s %v", out, err)
	}
	if len(desks) != 0 {
		t.Fatal(desks)
	}
	now = time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	out, err = (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"morning","importance":"high"}`))
	if err != nil || !strings.Contains(out, "quiet-hours batch") {
		t.Fatalf("%s %v", out, err)
	}
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || !strings.Contains(notes[0].Text, "Quiet hours batch:") || !strings.Contains(notes[0].Text, "ordinary") || !strings.Contains(notes[0].Text, "urgent") || !strings.Contains(notes[0].Text, "also urgent") || notes[1].Text != "morning" {
		t.Fatalf("%+v", notes)
	}
	if len(desks) != 2 || !strings.Contains(desks[0], "Quiet hours batch:") || desks[1] != "morning" {
		t.Fatalf("desks %v", desks)
	}

	empty := ""
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &empty, QuietEnd: &empty}); err != nil {
		t.Fatal(err)
	}
	k.turn.turnSource = link.NameWebhook
	k.turn.turnTrusted = false
	out, err = (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"skip me","importance":"low"}`))
	if err != nil || out != "notified the owner" {
		t.Fatalf("untrusted low %s %v", out, err)
	}
	k.turn.turnSource = link.NameTerminal
	k.turn.turnTrusted = true
	out, err = (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"owner chat","importance":"low"}`))
	if err != nil || out != "notified the owner" {
		t.Fatalf("chat %s %v", out, err)
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

func TestNotifyHeldAtShanghaiEvening(t *testing.T) {
	b := testBot(t)
	start, end, tz := "18:00", "23:59", "Asia/Shanghai"
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &start, QuietEnd: &end, Timezone: &tz}); err != nil {
		t.Fatal(err)
	}
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 18, 44, 0, 0, sh)
	k := &Kernel{Bot: b, Now: func() time.Time { return now }, turn: &turn{turnSource: link.NameTimer}}
	out, err := (notifyTool{k}).Run(context.Background(), json.RawMessage(`{"text":"evening"}`))
	if err != nil || out != "held until quiet hours end" {
		t.Fatalf("%s %v", out, err)
	}
}

func transcriptHas(entries []bot.Entry, text string) bool {
	for _, e := range entries {
		if strings.Contains(e.Message.Content, text) {
			return true
		}
	}
	return false
}
