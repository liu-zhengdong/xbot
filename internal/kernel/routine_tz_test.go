package kernel

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"xbot/internal/link"
)

const routineTZMarker = "XBOT_ROUTINE_TZ_CHILD"

// A cron routine created through the tool fires in the computer's local zone.
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
	raw, err := json.Marshal(map[string]string{"id": "r1", "name": "morning", "prompt": "ping", "cron": "0 9 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := (routineCreate{k}).Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
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
	if !strings.Contains(out, "local time") {
		t.Fatalf("result %q", out)
	}
}
