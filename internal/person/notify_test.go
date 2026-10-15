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
	line := LogEntry{Time: at.Add(time.Hour), Device: "other", Person: p, Grant: true}
	if err := MergeLog(root, []LogEntry{line}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("relayed merge writes %d", n)
	}
	if err := MergeLog(root, []LogEntry{line}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("duplicate merge writes %d", n)
	}
	got, err := Load(root, "u1")
	if err != nil || got.DisplayName != "Beatrice" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, a := range got.Accounts {
		if a.Link != "" {
			t.Fatalf("grant leaked %+v", got.Accounts)
		}
	}
}
