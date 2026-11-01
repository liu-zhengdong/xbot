package kernel

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"xbot/internal/cliops"
	"xbot/internal/link"
)

const routineTZMarker = "XBOT_ROUTINE_TZ_CHILD"

// A cron routine created through the runtime handler fires in the computer's local zone.
// The child re-execs the test binary with TZ=Asia/Shanghai, so time.Local is
// never mutated in this process.
func TestRoutineCreateUsesLocalZone(t *testing.T) {
	if os.Getenv(routineTZMarker) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRoutineCreateUsesLocalZone$")
		cmd.Env = append(os.Environ(), routineTZMarker+"=1", "TZ=Asia/Shanghai")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "PASS") {
			t.Fatalf("child did not pass:\n%s", out)
		}
		return
	}

	b := testBot(t, link.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}
	k := &Kernel{Bot: b, Links: []link.Link{tm}, Now: func() time.Time { return time.Now().UTC() }}
	out, err := k.RoutineHandlers().Create(context.Background(), cliops.RoutineCreate{
		ID: "r1", Name: "morning", Prompt: "ping", Cron: "0 9 * * *",
	})
	if err != nil || !out.Applied || out.ID != "r1" || out.Stage != "complete" || out.Error != "" {
		t.Fatalf("create %+v: %v", out, err)
	}
	fire := func(when time.Time) []link.Event {
		t.Helper()
		var posted []link.Event
		if err := tm.Fire(when, func(ev link.Event) error {
			posted = append(posted, ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return posted
	}
	if got := fire(time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("fired in UTC: %+v", got)
	}
	if got := fire(time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)); len(got) != 1 {
		t.Fatalf("09:00 local should fire at 01:00Z: %+v", got)
	}
	saved, err := tm.Load("r1")
	if err != nil || saved.Zone != "local" {
		t.Fatalf("cron zone %+v: %v", saved, err)
	}
}
