package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/cliops"
	"xbot/internal/inbox"
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
		if err := bus.PostGroup(inbox.KindBot, sender, g.ID, text, "ch_t", 1, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := bus.Transcript(g.ID)
	if err != nil || len(lines) != 60 {
		t.Fatalf("fixture transcript: %d messages, %v", len(lines), err)
	}
	check := func(reply cliops.ChatGroupHistoryReply, want []peer.Line) {
		t.Helper()
		if reply.Group == nil || reply.Group.ID != g.ID || reply.Group.Name != g.Name || !slices.Equal(reply.Group.Members, g.Members) || !reply.Group.Created.Equal(g.Created) {
			t.Fatalf("full group metadata: %+v, want %+v", reply.Group, g)
		}
		if !reply.Untrusted || reply.Count != len(want) || reply.Messages == nil || len(*reply.Messages) != len(want) || reply.File != "" || reply.Error != "" {
			t.Fatalf("inline untrusted history: %+v, want %d messages", reply, len(want))
		}
		out := cliops.FormatChatGroupHistory(reply)
		rows := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		if len(rows) != len(want)+3 || !strings.Contains(strings.ToLower(rows[0]), "untrusted") || rows[1] != fmt.Sprintf("count: %d", len(want)) {
			t.Fatalf("untrusted banner and one physical row per message:\n%s", out)
		}
		for i, line := range *reply.Messages {
			expected := want[i]
			if line.ID != expected.ID || !line.Time.Equal(expected.Time) || line.Sender != expected.Sender || line.Kind != expected.Kind || line.Text != expected.Text {
				t.Fatalf("full typed message %d: %+v, want %+v", i, line, expected)
			}
			fields := strings.Split(rows[i+3], "\t")
			if len(fields) != 5 {
				t.Fatalf("message %d has %d fields, want id/time/sender/kind/quoted text: %q", i, len(fields), rows[i+3])
			}
			text, err := strconv.Unquote(fields[4])
			if err != nil || fields[0] != expected.ID || fields[1] != expected.Time.Format(time.RFC3339) || fields[2] != expected.Sender || fields[3] != expected.Kind || text != expected.Text {
				t.Fatalf("formatted message %d: %q (unquote %v), want %+v", i, rows[i+3], err, expected)
			}
		}
	}

	k := &Kernel{Bot: ada}
	run := k.ChatGroupHandlers().History
	reply, err := run(context.Background(), cliops.ChatGroupHistory{ID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	check(reply, lines[40:]) // Default 20 newest, oldest first.
	reply, err = run(context.Background(), cliops.ChatGroupHistory{ID: g.ID, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	check(reply, lines[10:]) // Limit caps at 50.
	reply, err = run(context.Background(), cliops.ChatGroupHistory{ID: g.ID, Keyword: "release", Sender: "ada", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	check(reply, []peer.Line{lines[39], lines[49], lines[59]})
	first := (*reply.Messages)[0].ID
	reply, err = run(context.Background(), cliops.ChatGroupHistory{ID: g.ID, Sender: "ada", BeforeID: first})
	if err != nil {
		t.Fatal(err)
	}
	check(reply, []peer.Line{lines[9], lines[19], lines[29]}) // BeforeID is exclusive.

	reply, err = run(context.Background(), cliops.ChatGroupHistory{ID: g.ID, Keyword: "no matching message"})
	if err != nil {
		t.Fatal(err)
	}
	check(reply, nil)
	raw, err := json.Marshal(reply)
	if err != nil || !strings.Contains(string(raw), `"messages":[]`) {
		t.Fatalf("empty history must encode an empty array: %s, %v", raw, err)
	}

	if _, err := (&Kernel{Bot: eve}).ChatGroupHandlers().History(context.Background(), cliops.ChatGroupHistory{ID: g.ID}); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("non-member: %v", err)
	}
}
