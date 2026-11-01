package web

import (
	"context"
	"os"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestBotInfoCompletionReadUsesRetainedSourceCurrentRoute(t *testing.T) {
	f := newMutationHostFixture(t)
	f.s.owner = "u1"
	if err := f.target.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	completion := bot.Entry{Time: time.Now(), Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(link.Render(link.Event{Source: link.SourceSubagent, Sender: "sa_done", Type: "done", Content: "completed", Untrusted: true}, false))}}
	if err := f.target.Append(completion); err != nil {
		t.Fatal(err)
	}
	k, _, err := f.s.kernel(f.target)
	if err != nil {
		t.Fatal(err)
	}
	k.Statuses = statusSource{f.s}
	entry := f.s.kernels[f.target.Name()]
	previous := k.Bot
	next := "renamed"
	receipt, err := f.s.RenameResources(k, bot.ProfileChange{Name: &next})
	if err != nil || !receipt.Moved {
		t.Fatalf("rename %+v %v", receipt, err)
	}
	if f.s.kernels[next] != entry || entry.Kernel != k || k.Bot != previous || k.Bot.Name() != f.target.Name() {
		t.Fatal("fixture must retain the original source Kernel and old Bot snapshot")
	}
	oldPath := bot.Dir(f.s.Root, f.target.Name())
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("rename did not remove old path: %v", err)
	}
	for i := 0; i < 2; i++ {
		reply, err := k.BotHandlers().Info(context.Background(), cliops.BotInfoQuery{Bot: next})
		if err != nil || reply.Bots == nil || len(*reply.Bots) != 1 {
			t.Fatalf("actual host-backed Info %+v %v", reply, err)
		}
		if got := (*reply.Bots)[0].CompletedUnread; got != 1 {
			t.Errorf("poll %d: current completion count=%d, want1", i, got)
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("poll %d: completion read recreated original transcript directory: %v", i, err)
		}
	}
	current, err := k.Resources.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	count, err := current.Count()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.writeCursor(next, count); err != nil {
		t.Fatal(err)
	}
	reply, err := k.BotHandlers().Info(context.Background(), cliops.BotInfoQuery{Bot: next})
	if err != nil || reply.Bots == nil || len(*reply.Bots) != 1 || (*reply.Bots)[0].CompletedUnread != 0 {
		t.Fatalf("owner cursor no longer clears current completions %+v %v", reply, err)
	}
}
