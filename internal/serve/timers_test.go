package serve

import (
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func timerBot(t *testing.T) *bot.Bot {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "ada", "Be brief.", []string{link.NameTimer})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDeliverTimersPostsAndExpires(t *testing.T) {
	b := timerBot(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(b.RoutineDir())))
	dir := b.RoutineDir()
	tm := &link.Timer{Dir: dir}
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	if err := tm.Save(link.Routine{ID: "cron", Name: "c", Prompt: "p", Cron: "0 * * * *", Zone: "UTC", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Save(link.Routine{ID: "missed", Name: "m", Prompt: "p", At: now.Add(-3 * time.Minute).Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Save(link.Routine{ID: "late", Name: "l", Prompt: "p", At: now.Add(-90 * time.Second).Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := DeliverTimers(root, now); err != nil {
		t.Fatal(err)
	}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := in.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending %+v", pending)
	}
	// A second call must not duplicate the cron event.
	if err := DeliverTimers(root, now); err != nil {
		t.Fatal(err)
	}
	in, err = inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(in.Pending()); n != 2 {
		t.Fatalf("duplicated: %d", n)
	}
	got, err := tm.Load("missed")
	if err != nil || got.Enabled {
		t.Fatalf("missed one-shot %+v %v", got, err)
	}
	found := false
	for _, ev := range pending {
		if ev.ID == "rt_late_20261018T0958" {
			found = true
		}
	}
	if !found {
		t.Fatalf("90s late one-shot did not fire: %+v", pending)
	}
	got, err = tm.Load("late")
	if err != nil || got.LastSlot == "" {
		t.Fatalf("90s late one-shot %+v %v", got, err)
	}
}

func TestDeliverTimersSkipsBotsWithoutTimer(t *testing.T) {
	b, err := bot.Create(t.TempDir(), "ada", "Be brief.", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(b.RoutineDir())))
	if err := DeliverTimers(root, time.Now()); err != nil {
		t.Fatal(err)
	}
}
