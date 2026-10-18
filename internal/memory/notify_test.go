package memory

import (
	"context"
	"testing"
	"time"
)

func TestOnChangeFromLocalLog(t *testing.T) {
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
	fact := Fact{
		Key: "city", Scope: ScopePerson, PersonID: "u1", Tier: TierProfile,
		WrittenBy: "ada", Text: "Shanghai", Time: at,
	}
	if err := Open(root).Remember(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("remember writes %d", n)
	}
	if err := Open(root).Forget(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"}, "Shanghai"); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("forget writes %d", n)
	}
}
