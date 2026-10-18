package person

import (
	"testing"
	"time"
)

func TestOnChangeFromRecord(t *testing.T) {
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
	if err := Record(root, "ada", "dev", at.Add(time.Hour), p); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("second write %d", n)
	}
	got, err := Load(root, "u1")
	if err != nil || got.DisplayName != "Beatrice" || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("%+v %v", got, err)
	}
	// A local identify appends to the same log and notifies once.
	if _, err := Identify(root, Account{Link: "slack", ExternalID: "b"}, "Bea"); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("identify writes %d", n)
	}
}
