package person

import (
	"testing"
	"time"
)

func TestOnChangeSkipsMerge(t *testing.T) {
	root := t.TempDir()
	var n int
	OnChange(func(got string) {
		if got != root {
			t.Errorf("root %s", got)
		}
		n++
	})
	t.Cleanup(func() { OnChange(nil) })
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	p := Person{ID: "u1", DisplayName: "Ada", Timezone: "Asia/Shanghai"}
	if err := Record(root, "ada", "dev", at, p); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("writes %d", n)
	}
	p.DisplayName = "Beatrice"
	if err := MergeLog(root, []LogEntry{{Time: at.Add(time.Hour), Device: "other", Person: p}}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("merge notified, writes %d", n)
	}
}
