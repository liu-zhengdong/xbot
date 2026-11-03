package proc

import (
	"context"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/mcp"
	"xbot/internal/provider"
)

type legacyRecoveryModel struct {
	calls     int
	sawReport bool
}

func (p *legacyRecoveryModel) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	for _, msg := range req.Messages {
		p.sawReport = p.sawReport || strings.Contains(msg.Text(), "因进程重启中断")
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}, nil
}

func TestDrainRepairsLegacyForkBeforeReadingEmptyInbox(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, err := bot.Create(t.TempDir(), "legacy", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old task")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_restart", 1, "legacy task"); err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := kernel.NewBotResources(ctx, b, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	var box *inbox.Inbox
	if err := resources.WithCurrentIO(func(_ *bot.Bot, current *inbox.Inbox) error { box = current; return nil }); err != nil {
		t.Fatal(err)
	}
	model := &legacyRecoveryModel{}
	k := &kernel.Kernel{Bot: b, Inbox: box, Resources: resources, Provider: model, MaxSteps: 2}
	if err := k.RecoverTasks(); err != nil {
		t.Fatal(err)
	}
	if len(box.Pending()) != 0 {
		t.Fatal("openKernel recovery posted legacy work before the handle phase")
	}
	lock, err := b.LockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := Drain(ctx, k); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || !model.sawReport || len(box.Pending()) != 0 {
		t.Fatalf("empty-inbox recovery was not consumed in the same pass: calls=%d report=%v pending=%d", model.calls, model.sawReport, len(box.Pending()))
	}
	fresh := &kernel.Kernel{Bot: b, Inbox: box, Resources: resources, Provider: model, MaxSteps: 2}
	if err := Drain(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 {
		t.Fatal("a replacement Kernel duplicated the legacy recovery report")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	for _, entry := range entries {
		if entry.Mark != nil && entry.Mark.Kind == bot.ForkKindEnd && entry.Mark.Branch == "bg_restart" {
			closed++
			if entry.Mark.Status != bot.ForkInterrupted {
				t.Fatalf("unexpected legacy close: %+v", entry.Mark)
			}
		}
	}
	if closed != 1 {
		t.Fatalf("legacy close count=%d", closed)
	}
}
