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
	return Entry{Message: provider.Message{Role: provider.RoleUser, Content: content}, Branch: branch}
}

func mainRow(content string) Entry {
	return Entry{Message: provider.Message{Role: provider.RoleUser, Content: content}}
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
	in := Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"}, Branch: "bg_9"}
	rows, _ := Replay([]SeqEntry{{Seq: 4, Entry: in}})
	if !reflect.DeepEqual(rows[0].Entry, in) || rows[0].Seq != 4 || rows[0].Branch != "bg_9" {
		t.Fatalf("row %+v", rows[0])
	}
}
