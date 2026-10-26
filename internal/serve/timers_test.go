package serve

import (
	"path/filepath"
	"sync"
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

func TestDeliverTimersPrecheckConcurrent(t *testing.T) {
	for _, command := range []string{"", "true", "false"} {
		t.Run(command, func(t *testing.T) {
			b := timerBot(t)
			root := filepath.Dir(filepath.Dir(filepath.Dir(b.RoutineDir())))
			tm := &link.Timer{Dir: b.RoutineDir()}
			now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
			if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: command, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := DeliverTimers(root, now); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			in, err := inbox.Open(b.InboxPath())
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if command == "false" {
				want = 0
			}
			if got := len(in.Pending()); got != want {
				t.Fatalf("inbox %d, want %d", got, want)
			}
			r, err := tm.Load("check")
			if err != nil || r.LastSlot != "20261027T0800" {
				t.Fatalf("slot: %+v %v", r, err)
			}
		})
	}
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
