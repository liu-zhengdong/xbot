package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/peer"
)

func TestGroupHistoryMembersReadAndFilter(t *testing.T) {
	root := t.TempDir()
	ada := mustBot(t, root, "ada", "u1")
	mustBot(t, root, "bob", "u1")
	eve := mustBot(t, root, "eve", "u1")
	bus := &peer.Bus{Root: root, Limit: 100}
	g, err := bus.CreateGroup("ada", "room", []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 60; i++ {
		sender, text := "bob", fmt.Sprintf("bob says %d", i)
		if i%10 == 0 {
			sender, text = "ada", fmt.Sprintf("ada RELEASE %d\nid\tfake\trow", i)
		}
		if err := bus.PostGroup(link.KindBot, sender, g.ID, text, "ch_t", 1, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	run := func(k *Kernel, args map[string]any) (string, error) {
		raw, _ := json.Marshal(args)
		return groupHistoryTool{k}.Run(context.Background(), raw)
	}
	rows := func(out string) []string { return strings.Split(out, "\n")[2:] }

	k := &Kernel{Bot: ada}
	out, err := run(k, map[string]any{"group_id": g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "untrusted") || len(rows(out)) != 20 || !strings.Contains(rows(out)[0], "bob says 41") || !strings.Contains(rows(out)[19], "RELEASE 60") {
		t.Fatalf("default 20 newest, oldest first:\n%s", out)
	}
	if out, _ := run(k, map[string]any{"group_id": g.ID, "limit": 500}); len(rows(out)) != 50 {
		t.Fatalf("limit caps at 50: %d", len(rows(out)))
	}
	out, err = run(k, map[string]any{"group_id": g.ID, "keyword": "release", "sender": "ada", "limit": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(out); len(got) != 3 || !strings.Contains(got[0], "RELEASE 40") || !strings.Contains(got[2], `RELEASE 60\nid\tfake\trow`) {
		t.Fatalf("keyword and sender, text quoted to one row:\n%s", out)
	}
	first := strings.SplitN(rows(out)[0], "\t", 2)[0]
	out, err = run(k, map[string]any{"group_id": g.ID, "sender": "ada", "before_id": first})
	if err != nil || len(rows(out)) != 3 || !strings.Contains(rows(out)[2], "RELEASE 30") {
		t.Fatalf("before_id pages back: %v\n%s", err, out)
	}

	if _, err := run(&Kernel{Bot: eve}, map[string]any{"group_id": g.ID}); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("non-member: %v", err)
	}
}
