package proc

import (
	"testing"
	"time"

	"xbot/internal/link"
)

func TestDeliverDuePrecheck(t *testing.T) {
	for _, command := range []string{"", "true", "false"} {
		t.Run(command, func(t *testing.T) {
			tm, k, _ := chatTimer(t)
			now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
			saveRoutine(t, tm, link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: command, Enabled: true})
			for i := 0; i < 2; i++ {
				if err := DeliverDue(k, now); err != nil {
					t.Fatal(err)
				}
			}
			want := 1
			if command == "false" {
				want = 0
			}
			if got := len(k.Inbox.Pending()); got != want {
				t.Fatalf("inbox %d, want %d", got, want)
			}
			r, err := tm.Load("check")
			if err != nil || r.LastSlot != "20261027T0800" {
				t.Fatalf("slot: %+v %v", r, err)
			}
		})
	}
}
