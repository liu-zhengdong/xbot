package memory

import (
	"context"
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
	fact := Fact{
		Key: "city", Scope: ScopePerson, PersonID: "u1", Tier: TierProfile,
		WrittenBy: "ada", Text: "Shanghai", Time: at,
	}
	if err := Open(root).Remember(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("writes %d", n)
	}
	fact.Text = "Beijing"
	fact.Time = at.Add(time.Hour)
	fact.WrittenBy = "bob"
	if err := MergeSync(root, []SyncLine{{Time: fact.Time, Bot: "bob", Device: "other", Op: opRemember, Fact: fact}}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("merge notified, writes %d", n)
	}
}
