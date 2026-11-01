package cliops

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestChatGroupRuntimeFormatsAndTypedJSON(t *testing.T) {
	at := time.Date(2026, 11, 2, 1, 2, 3, 456789, time.FixedZone("offset", 8*3600))
	group := ChatGroup{ID: "g_full", Name: "quote\tname\r\x01", Members: []string{"A", "B"}, Created: at}
	line := ChatGroupLine{ID: "gm_full", Time: at, Sender: "A", Kind: "bot", Text: "first\nforged\tline\r\x00"}
	lines := []ChatGroupLine{line}
	reply := ChatGroupHistoryReply{Group: &group, Messages: &lines, Untrusted: true, Count: 1}
	text := FormatChatGroupHistory(reply)
	if !strings.Contains(text, "UNTRUSTED") || !strings.Contains(text, strconv.Quote(group.Name)) || !strings.Contains(text, strconv.Quote(line.Text)) || !strings.Contains(text, "gm_full\t"+at.Format(time.RFC3339)+"\tA\tbot\t") || strings.Count(text, "\n") != 4 {
		t.Fatalf("history=%q", text)
	}
	wire, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var restored ChatGroupHistoryReply
	if err := json.Unmarshal(wire, &restored); err != nil || restored.Messages == nil || !(*restored.Messages)[0].Time.Equal(at) || (*restored.Messages)[0].Text != line.Text || restored.Group.Created.Nanosecond() != at.Nanosecond() {
		t.Fatalf("time or fields lost: %s err=%v", wire, err)
	}
	emptyGroups, emptyLines := []ChatGroup{}, []ChatGroupLine{}
	for _, value := range []any{ChatGroupListReply{Groups: &emptyGroups}, ChatGroupHistoryReply{Messages: &emptyLines, Untrusted: true}, ChatGroupPostReply{PostedMembers: []string{}}} {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), "null") || !strings.Contains(string(raw), "[]") {
			t.Fatalf("empty arrays=%s", raw)
		}
	}
	for _, text := range []string{FormatChatGroupList(ChatGroupListReply{Count: 7, File: "/work/results/groups.json", Access: "local", Bytes: 4321}), FormatChatGroupCreate(ChatGroupCreateReply{Group: &group, Created: true}), FormatChatGroupPost(ChatGroupPostReply{ID: "g", MessageID: "gm", Appended: true, PostedMembers: []string{"B"}, FailedMember: "C", Stage: "recipient_open", Error: "failed"})} {
		if !strings.HasSuffix(text, "\n") {
			t.Fatalf("missing newline %q", text)
		}
	}
}
