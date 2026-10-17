package eval

import (
	"testing"
	"time"

	"xbot/internal/link"
)

func TestWakeEventRoutineUsesSharedBody(t *testing.T) {
	c := Case{Name: "routine-fallback", Wake: Wake{Kind: "routine", Text: "没有内容就说今天还没聊"}}
	when := time.Date(2026, 10, 15, 8, 7, 0, 0, time.UTC)
	ev := wakeEvent(c, when)
	want := link.RoutineBody("eval", c.Name, renderTask(c.Wake.Text, nil))
	if ev.Content != want {
		t.Fatalf("routine body drifted from link.RoutineBody:\ngot  %q\nwant %q", ev.Content, want)
	}
}
