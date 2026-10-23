package bot

import (
	"reflect"
	"testing"

	"xbot/internal/provider"
)

func replayEntries(entries ...Entry) []SeqEntry {
	out := make([]SeqEntry, len(entries))
	for i, e := range entries {
		out[i] = SeqEntry{Seq: i + 1, Entry: e}
	}
	return out
}

func branchRow(content, branch string) Entry {
	return Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(content)}, Branch: branch}
}

func mainRow(content string) Entry {
	return Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(content)}}
}

func forkRow(branch string, from int, task string) Entry {
	return Entry{Message: provider.Message{Role: provider.RoleUser}, Mark: &ForkMark{Kind: ForkKindStart, Branch: branch, From: from, Task: task}}
}

func forkEndRow(branch, status string, steps int) Entry {
	return Entry{Message: provider.Message{Role: provider.RoleUser}, Mark: &ForkMark{Kind: ForkKindEnd, Branch: branch, Status: status, Steps: steps}}
}

func TestReplayNoForkAllMain(t *testing.T) {
	rows, branches := Replay(replayEntries(mainRow("a"), mainRow("b"), mainRow("c")))
	if len(rows) != 3 {
		t.Fatalf("rows %d", len(rows))
	}
	for _, r := range rows {
		if r.Branch != "" {
			t.Fatalf("seq %d branch %q", r.Seq, r.Branch)
		}
	}
	if len(branches) != 0 {
		t.Fatalf("branches %+v", branches)
	}
}

func TestReplayOpenFork(t *testing.T) {
	rows, branches := Replay(replayEntries(
		mainRow("start"),
		mainRow("forked assistant"),
		forkRow("bg_7", 1, "long job"),
		branchRow("after", "bg_7"),
	))
	want := []string{"", "bg_7", "bg_7", "bg_7"}
	for i, r := range rows {
		if r.Branch != want[i] {
			t.Fatalf("seq %d branch %q want %q", r.Seq, r.Branch, want[i])
		}
	}
	st, ok := branches["bg_7"]
	if !ok || st.Task != "long job" || !st.Open || st.Status != "" || st.Steps != 0 {
		t.Fatalf("state %+v", st)
	}
}

func TestReplayClosedFork(t *testing.T) {
	rows, branches := Replay(replayEntries(
		mainRow("start"),
		forkRow("bg_7", 1, "long job"),
		branchRow("after", "bg_7"),
		forkEndRow("bg_7", ForkDone, 3),
		mainRow("owner reply"),
	))
	want := []string{"", "bg_7", "bg_7", "bg_7", ""}
	for i, r := range rows {
		if r.Branch != want[i] {
			t.Fatalf("seq %d branch %q want %q", r.Seq, r.Branch, want[i])
		}
	}
	st := branches["bg_7"]
	if st.Open || st.Status != ForkDone || st.Steps != 3 || st.Task != "long job" {
		t.Fatalf("state %+v", st)
	}
}

func TestReplayInterleavedMainAndBranch(t *testing.T) {
	rows, branches := Replay(replayEntries(
		mainRow("start"),
		mainRow("forked tool call"),
		forkRow("bg_7", 1, "long job"),
		mainRow("owner interrupt"),
		branchRow("background step", "bg_7"),
		mainRow("owner again"),
		forkEndRow("bg_7", ForkCancelled, 1),
	))
	want := []string{"", "bg_7", "bg_7", "", "bg_7", "", "bg_7"}
	if len(rows) != len(want) {
		t.Fatalf("rows %d", len(rows))
	}
	for i, r := range rows {
		if r.Branch != want[i] {
			t.Fatalf("seq %d branch %q want %q", r.Seq, r.Branch, want[i])
		}
	}
	if st := branches["bg_7"]; st.Open || st.Status != ForkCancelled || st.Steps != 1 {
		t.Fatalf("state %+v", st)
	}
}

func TestReplayKeepsEntryPayload(t *testing.T) {
	in := Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("hi")}, Branch: "bg_9"}
	rows, _ := Replay([]SeqEntry{{Seq: 4, Entry: in}})
	if !reflect.DeepEqual(rows[0].Entry, in) || rows[0].Seq != 4 || rows[0].Branch != "bg_9" {
		t.Fatalf("row %+v", rows[0])
	}
}

// Two backgrounds run at once: bg_1 forked from the first turn, then the foreground's
// own turn forked again as bg_2 while bg_1 kept writing tagged rows.
func TestReplayTwoConcurrentBranches(t *testing.T) {
	rows, branches := Replay(replayEntries(
		mainRow("task one"),           // 1 main
		mainRow("one step"),           // 2 bg_1 (pre-marker)
		forkRow("bg_1", 1, "one"),     // 3
		mainRow("task two"),           // 4 main (foreground)
		branchRow("one more", "bg_1"), // 5
		mainRow("two step"),           // 6 bg_2 (pre-marker)
		forkRow("bg_2", 4, "two"),     // 7
		branchRow("two more", "bg_2"), // 8
		branchRow("one done", "bg_1"), // 9
		forkEndRow("bg_1", "done", 3),
		mainRow("chat"), // 11 main
	))
	want := []string{"", "bg_1", "bg_1", "", "bg_1", "bg_2", "bg_2", "bg_2", "bg_1", "bg_1", ""}
	for i, r := range rows {
		if r.Branch != want[i] {
			t.Fatalf("seq %d branch %q want %q", r.Seq, r.Branch, want[i])
		}
	}
	if b := branches["bg_1"]; b.Open || b.Status != "done" {
		t.Fatalf("bg_1 %+v", b)
	}
	if b := branches["bg_2"]; !b.Open || b.Task != "two" {
		t.Fatalf("bg_2 %+v", b)
	}
}
