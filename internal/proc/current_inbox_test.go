package proc

import (
	"context"
	"os"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/provider"
)

type renameInProcHandle struct {
	rename func() error
	calls  int
}

func (p *renameInProcHandle) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	p.calls++
	if p.calls == 1 {
		if err := p.rename(); err != nil {
			return provider.Message{}, err
		}
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}, nil
}

func TestProcRetainedInboxFollowsHandleRename(t *testing.T) {
	for _, mode := range []string{"drain", "run"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			root := t.TempDir()
			b, err := bot.Create(root, "before", "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			oldBox, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			seed := link.NewEvent(link.NameTimer, link.TypeTimer, "fixture wake", "u1", at)
			if err := oldBox.Post(seed); err != nil {
				t.Fatal(err)
			}
			manager := mcp.NewManager(ctx, mcp.Options{})
			defer manager.Close()
			resources, err := kernel.NewBotResources(ctx, b, manager)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.Close()
			tm := &link.Timer{Dir: b.RoutineDir(), Sender: "u1", Loc: time.UTC}
			tm.WithDirectoryIO = func(fn func(string) error) error {
				return resources.WithCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error { return fn(current.RoutineDir()) })
			}
			if err := tm.Save(link.Routine{ID: "once", Prompt: "scheduled fixture", At: at.Format(time.RFC3339), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			model := &renameInProcHandle{}
			k := &kernel.Kernel{Bot: b, Inbox: oldBox, Resources: resources, Provider: model, Links: []link.Link{tm}, MaxSteps: 1}
			model.rename = func() error { return k.RenameRuntime("after") }
			if mode == "drain" {
				hold, err := b.LockHandle()
				if err != nil {
					t.Fatal(err)
				}
				err = Drain(ctx, k)
				_ = hold.Release()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				runCtx, stop := context.WithCancel(ctx)
				defer stop()
				handled := false
				err := Run(runCtx, Config{K: k, SkipDeliver: true, After: func(_ link.Event, err error) {
					if err != nil {
						t.Error(err)
					}
					handled = true
				}, Idle: func() {
					if handled {
						stop()
					}
				}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if k.Bot.Name() != "before" || k.Inbox != oldBox || resources.Name() != "after" {
				t.Fatal("fixture did not retain the original Kernel handles")
			}
			if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
				t.Fatal("proc ack/reload revived the old Inbox directory", err)
			}
			// A second drain must reload the moved log and observe its actual ack.
			if err := Drain(ctx, k); err != nil {
				t.Fatal(err)
			}
			if model.calls != 1 {
				t.Fatal("acked event replayed after rename", model.calls)
			}
			current, err := resources.CurrentBot()
			if err != nil {
				t.Fatal(err)
			}
			box, err := inbox.Open(current.InboxPath())
			if err != nil || len(box.Pending()) != 0 {
				t.Fatal("ack missing from current Inbox", err)
			}
			// Keep the cached Timer unchanged: delivery must route its post anew.
			if err := DeliverDue(k, at); err != nil {
				t.Fatal(err)
			}
			if err := box.Reload(); err != nil {
				t.Fatal(err)
			}
			pending := box.Pending()
			if len(pending) != 1 || pending[0].Source != link.NameTimer {
				t.Fatal("timer post missed current Inbox", pending)
			}
			if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
				entries, readErr := os.ReadDir(bot.Dir(root, "before"))
				t.Fatalf("timer delivery revived the old directory: entries=%v stat=%v read=%v", entries, err, readErr)
			}
		})
	}
}
