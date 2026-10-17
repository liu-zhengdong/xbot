package proc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
)

func TestMissedUsesZone(t *testing.T) {
	tm := &link.Timer{Dir: t.TempDir(), Loc: time.FixedZone("CST", 8*3600)}
	if err := tm.Save(link.Routine{ID: "local", Prompt: "p", Cron: "0 9 * * *", Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Save(link.Routine{ID: "legacy", Prompt: "p", Cron: "0 9 * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	got, err := missedSlots(tm, since, since.Add(2*time.Hour), since.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "local" || !got[0].At.Equal(since.Add(time.Hour)) {
		t.Fatalf("slots: %+v", got)
	}
}

// chatTimer uses the real chat delivery path without running a model.
func chatTimer(t *testing.T) (*link.Timer, *kernel.Kernel, string) {
	t.Helper()
	dir := t.TempDir()
	in, err := inbox.Open(filepath.Join(dir, "inbox"))
	if err != nil {
		t.Fatal(err)
	}
	tm := &link.Timer{Dir: filepath.Join(dir, "routines"), Loc: time.UTC}
	return tm, &kernel.Kernel{Inbox: in, Links: []link.Link{tm}}, filepath.Join(dir, "timer-watermark")
}

func saveRoutine(t *testing.T, tm *link.Timer, r link.Routine) {
	t.Helper()
	if err := tm.Save(r); err != nil {
		t.Fatal(err)
	}
}

func TestChatNoticesBeforeExpiringOneShot(t *testing.T) {
	tm, k, path := chatTimer(t)
	start := time.Date(2026, 10, 18, 10, 15, 17, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "once", Prompt: "p", At: start.Add(time.Hour).Format(time.RFC3339), Enabled: true})
	if err := DeliverDue(k, start); err != nil {
		t.Fatal(err)
	}
	if err := DeliverDue(k, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	evs := k.Inbox.Pending()
	if len(evs) != 1 || evs[0].ID != "missed_20261018T101517" || !strings.Contains(evs[0].Content, `- "once"`) {
		t.Fatalf("notice: %+v", evs)
	}
	r, err := tm.Load("once")
	if err != nil || r.Enabled {
		t.Fatalf("one-shot not expired: %+v %v", r, err)
	}
	if err := NoticeMissed(tm, k.Inbox.Post, start.Add(2*time.Hour+15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(k.Inbox.Pending()) != 1 {
		t.Fatal("second poller duplicated notice")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("watermark mode: %v %v", info, err)
	}
}

func TestChatPollsAdvanceWatermark(t *testing.T) {
	tm, k, _ := chatTimer(t)
	start := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "cron", Prompt: "p", Cron: "0 * * * *", Zone: "UTC", Enabled: true})
	for i := 0; i <= 64; i++ {
		if err := DeliverDue(k, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := NoticeMissed(tm, k.Inbox.Post, start.Add(2*time.Hour+4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	evs := k.Inbox.Pending()
	if len(evs) != 3 || !strings.Contains(evs[2].Content, "12:00") || strings.Contains(evs[2].Content, "10:00") || strings.Contains(evs[2].Content, "11:00") {
		t.Fatalf("chat slot reported again: %+v", evs)
	}
}

func TestWatermarkReadFailureStillDelivers(t *testing.T) {
	tm, k, path := chatTimer(t)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "cron", Prompt: "p", Cron: "0 * * * *", Enabled: true})
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := NoticeMissed(tm, k.Inbox.Post, now); err == nil {
		t.Fatal("expected read error")
	}
	if err := DeliverDue(k, now); err != nil || len(k.Inbox.Pending()) != 1 {
		t.Fatalf("delivery blocked: %v", err)
	}
}

func TestWatermarkNeverMovesBackwards(t *testing.T) {
	tm, k, path := chatTimer(t)
	now := time.Date(2026, 10, 18, 10, 0, 17, 0, time.UTC)
	for _, at := range []time.Time{now, now.Add(-time.Hour)} {
		if err := NoticeMissed(tm, k.Inbox.Post, at); err != nil {
			t.Fatal(err)
		}
	}
	wm, ok, err := readWatermark(path)
	if err != nil || !ok || !wm.Equal(now) || len(k.Inbox.Pending()) != 0 {
		t.Fatalf("watermark: %v %v %v", wm, ok, err)
	}
}

func TestMissedBodySanitizesNames(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	body := missedBody([]missedSlot{
		{ID: "newline", Name: "real\n- fake\t", At: now},
		{ID: "long", Name: strings.Repeat("界", 300), At: now},
	}, now.Add(-time.Hour), now, true, time.UTC)
	if strings.Count(body, "\n- ") != 2 || !strings.Contains(body, `"real - fake "`) || !strings.Contains(body, strings.Repeat("界", 80)+"…") || strings.Contains(body, strings.Repeat("界", 81)) || strings.Contains(body, "</missed_routines>") {
		t.Fatal(body)
	}
}

func TestMissedSkipsFreshOneShot(t *testing.T) {
	tm, k, _ := chatTimer(t)
	now := time.Date(2026, 10, 18, 10, 0, 30, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "fresh", Prompt: "p", At: now.Add(-link.OneShotWindow).Format(time.RFC3339), Enabled: true})
	if err := DeliverDue(k, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := DeliverDue(k, now); err != nil {
		t.Fatal(err)
	}
	evs := k.Inbox.Pending()
	if len(evs) != 1 || strings.HasPrefix(evs[0].ID, "missed_") {
		t.Fatalf("fresh one-shot: %+v", evs)
	}
}

func TestMissedNoticeCapsAtSevenDays(t *testing.T) {
	tm, k, _ := chatTimer(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "daily", Prompt: "p", Cron: "0 9 * * *", Zone: "UTC", Enabled: true})
	for _, at := range []time.Time{start, start.AddDate(0, 0, 10)} {
		if err := NoticeMissed(tm, k.Inbox.Post, at); err != nil {
			t.Fatal(err)
		}
	}
	evs := k.Inbox.Pending()
	if len(evs) != 1 || !strings.Contains(evs[0].Content, `"daily" "": 7 missed`) || !strings.Contains(evs[0].Content, "Only the last 7 days") {
		t.Fatalf("cap: %+v", evs)
	}
}

func TestFailedNoticeKeepsOneShotForRetry(t *testing.T) {
	tm, k, path := chatTimer(t)
	start := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	saveRoutine(t, tm, link.Routine{ID: "once", Prompt: "p", At: start.Add(time.Hour).Format(time.RFC3339), Enabled: true})
	if err := DeliverDue(k, start); err != nil {
		t.Fatal(err)
	}
	// A directory where the lock file goes makes every notice attempt fail.
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err := DeliverDue(k, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if r, err := tm.Load("once"); err != nil || !r.Enabled {
		t.Fatalf("one-shot expired before its notice: %+v %v", r, err)
	}
}
