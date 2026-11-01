package cliops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPersonaRuntimeFormatReceipts(t *testing.T) {
	for _, tc := range []struct {
		reply       BotPersonaReply
		json, human string
	}{
		{BotPersonaReply{Name: "ada", Written: true, Effective: "next_wake"}, `{"name":"ada","written":true,"effective":"next_wake"}`, "name: ada\nwritten: true\neffective: next_wake\n"},
		{BotPersonaReply{Name: "ada", Error: "write failed"}, `{"name":"ada","written":false,"error":"write failed"}`, "name: ada\nwritten: false\nerror: write failed\n"},
		{BotPersonaReply{Name: "ada", Written: true, Effective: "next_wake", Error: "display failed"}, `{"name":"ada","written":true,"effective":"next_wake","error":"display failed"}`, "name: ada\nwritten: true\neffective: next_wake\nerror: display failed\n"},
	} {
		raw, err := json.Marshal(tc.reply)
		if err != nil || string(raw) != tc.json || FormatBotPersona(tc.reply) != tc.human {
			t.Fatalf("reply=%+v json=%s human=%q error=%v", tc.reply, raw, FormatBotPersona(tc.reply), err)
		}
	}
}

func TestBotFormatListExact(t *testing.T) {
	preview := strings.Repeat("p", 80) + "…"
	local := []BotListLocal{{Name: "ada", PersonaPreview: preview}}
	seen := time.Date(2026, 11, 1, 12, 30, 0, 123, time.FixedZone("source", 8*60*60))
	remote := []BotListRemote{{Name: "bea@laptop", Seen: seen}}
	reply := BotListReply{Local: &local, Remote: &remote, Count: 2}
	want := "count: 2\nlocal\tada\t" + preview + "\nremote\tbea@laptop\tseen 2026-11-01T12:30:00.000000123+08:00\n"
	if got := FormatBotList(reply); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	emptyLocal, emptyRemote := []BotListLocal{}, []BotListRemote{}
	raw, err := json.Marshal(BotListReply{Local: &emptyLocal, Remote: &emptyRemote})
	if err != nil || string(raw) != `{"local":[],"remote":[],"count":0}` {
		t.Fatalf("empty JSON=%s error=%v", raw, err)
	}
	artifact := BotListReply{Count: 100, File: "results/bots.json", Access: "local", Bytes: 123456, Error: "read failed"}
	if got := FormatBotList(artifact); got != "count: 100\nresults/bots.json (access: local, bytes: 123456)\nerror: read failed\n" {
		t.Fatalf("artifact=%q", got)
	}
}

func TestBotFormatInfoExactTypedFields(t *testing.T) {
	bots := []BotStatusView{{Bot: "ada", State: "running", Tool: "shell", ToolSummary: "building", Since: "2026-11-01T00:00:00Z", Backgrounds: 1, Agents: 2, CompletedUnread: 3, LastMessage: "hi", Todo: "test", Tokens: &BotTokenUse{Prompt: 11, Completion: 12, Cached: 13, TurnCompletion: 14, SessionPrompt: 15, SessionCompletion: 16, SessionCached: 17, Window: 18, Estimated: true}}}
	steps := []BotStepView{{Time: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Tool: "shell", State: "done", Summary: "built"}}
	reply := BotInfoReply{Bots: &bots, Steps: &steps, Count: 1}
	want := "count: 1\nrunning\tada\tsince 2026-11-01T00:00:00Z\ttool shell\tbuilding\tbackgrounds 1\tagents 2\tcompleted_unread 3\tlast: hi\ttodo: test\ntokens\tprompt 11\tcompletion 12\tcached 13\tturn_completion 14\tsession_prompt 15\tsession_completion 16\tsession_cached 17\twindow 18\testimated true\nstep\t2026-11-01T00:00:00Z\tdone\tshell\tbuilt\n"
	if got := FormatBotInfo(reply); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"bot", "state", "tool", "tool_summary", "since", "backgrounds", "agents", "completed_unread", "last_message", "todo", "tokens", "prompt", "completion", "cached", "turn_completion", "session_prompt", "session_completion", "session_cached", "window", "estimated", "time", "summary"} {
		if !strings.Contains(string(raw), `"`+field+`":`) {
			t.Errorf("missing typed field %q: %s", field, raw)
		}
	}
	if strings.Contains(string(raw), "callID") || strings.Contains(string(raw), "description") {
		t.Fatalf("private fields escaped: %s", raw)
	}
	if got := FormatBotInfo(BotInfoReply{Count: 9, File: "results/status.json", Access: "local", Bytes: 456, Error: "failed"}); got != "count: 9\nresults/status.json (access: local, bytes: 456)\nerror: failed\n" {
		t.Fatalf("artifact=%q", got)
	}
}

func TestBotControlFormatReceipts(t *testing.T) {
	pin := BotPinReply{Name: "ada", Pinned: false, Applied: false, Stage: "config", Error: "save failed"}
	if got := FormatBotPin(pin); got != "name: ada\npinned: false\napplied: false\nstage: config\nerror: save failed\n" {
		t.Fatalf("pin=%q", got)
	}
	raw, _ := json.Marshal(pin)
	if string(raw) != `{"name":"ada","pinned":false,"applied":false,"stage":"config","error":"save failed"}` {
		t.Fatalf("pin JSON=%s", raw)
	}
	primary := BotPrimaryReply{Name: "ada", Primary: "ada", Applied: true, Stage: "pin", Error: "pin failed"}
	if got := FormatBotPrimary(primary); got != "name: ada\nprimary: ada\napplied: true\nstage: pin\nerror: pin failed\n" {
		t.Fatalf("primary=%q", got)
	}
	raw, _ = json.Marshal(primary)
	if string(raw) != `{"name":"ada","primary":"ada","applied":true,"stage":"pin","error":"pin failed"}` {
		t.Fatalf("primary JSON=%s", raw)
	}
	uncommitted := BotPrimaryReply{Name: "ada", Stage: "primary"}
	raw, _ = json.Marshal(uncommitted)
	if string(raw) != `{"name":"ada","applied":false,"stage":"primary"}` || FormatBotPrimary(uncommitted) != "name: ada\napplied: false\nstage: primary\n" {
		t.Fatalf("uncommitted JSON=%s human=%q", raw, FormatBotPrimary(uncommitted))
	}
}

func TestBotFormatWriteReceipts(t *testing.T) {
	if got := FormatBotCreate(BotCreateReply{Name: "ada", Applied: true, Stage: "persona", Error: "too large"}); got != "name: ada\napplied: true\nstage: persona\nerror: too large\n" {
		t.Fatalf("create=%q", got)
	}
	if got := FormatBotDelete(BotDeleteReply{Name: "ada", Removed: true, Stage: "vault_key", Error: "failed"}); got != "name: ada\nremoved: true\nstage: vault_key\nerror: failed\n" {
		t.Fatalf("delete=%q", got)
	}
}
