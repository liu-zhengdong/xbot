package cliops

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestExperienceRuntimeExactRecordFormatAndNullableProtocol(t *testing.T) {
	confirmed := true
	for _, tc := range []struct {
		reply       ExperienceRecordReply
		human, wire string
	}{
		{ExperienceRecordReply{ID: 9, Recorded: &confirmed}, "recorded experience #9\n", `{"id":9,"from":0,"to":0,"recorded":true}`},
		{ExperienceRecordReply{ID: 12, From: 3, To: 8, Recorded: &confirmed}, "recorded experience #12 (seq 3-8)\n", `{"id":12,"from":3,"to":8,"recorded":true}`},
		{ExperienceRecordReply{}, "experience recording unconfirmed; database effects may have occurred\n", `{"from":0,"to":0,"recorded":null}`},
		{ExperienceRecordReply{ID: 17, From: 4, To: -1, Error: "insert outcome unknown"}, "experience recording unconfirmed #17 (seq 4--1); database effects may have occurred\nerror: insert outcome unknown\n", `{"id":17,"from":4,"to":-1,"recorded":null,"error":"insert outcome unknown"}`},
	} {
		if got := FormatExperienceRecord(tc.reply); got != tc.human {
			t.Errorf("human=%q want=%q", got, tc.human)
		}
		got, err := json.Marshal(tc.reply)
		if err != nil || string(got) != tc.wire {
			t.Errorf("wire=%s want=%s err=%v", got, tc.wire, err)
		}
	}
}

func TestExperienceRuntimeExactSearchFormatAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		reply ExperienceSearchReply
		want  string
	}{
		{ExperienceSearchReply{Text: "no matching experiences"}, "no matching experiences\n"},
		{ExperienceSearchReply{Text: "#1\nsummary\nsegment:\nuser: words\n\nCall detail with a seq to read one message in full."}, "#1\nsummary\nsegment:\nuser: words\n\nCall detail with a seq to read one message in full.\n"},
		{ExperienceSearchReply{Text: "original trailing newline\n"}, "original trailing newline\n\n"},
		{ExperienceSearchReply{File: "/current/results/experience.json", Access: "local", Bytes: 36712}, "/current/results/experience.json (access: local, bytes: 36712)\n"},
		{ExperienceSearchReply{File: "file", Access: "local", Bytes: 37, Error: "late failure"}, "file (access: local, bytes: 37)\nerror: late failure\n"},
	} {
		if got := FormatExperienceSearch(tc.reply); got != tc.want {
			t.Errorf("got=%q want=%q", got, tc.want)
		}
	}
}

func TestExperienceRuntimeDualOutputBudgetsPreserveFacts(t *testing.T) {
	confirmed := true
	for _, recorded := range []*bool{nil, &confirmed} {
		original := ExperienceRecordReply{ID: 9223372036854775807, From: -3, To: 700, Recorded: recorded, Error: strings.Repeat("\x00<&>", 12<<10)}
		got, err := boundExperienceRecordReply(original)
		raw, marshalErr := json.Marshal(got)
		if err == nil || marshalErr != nil || len(raw)+1 > experienceOutputBudget || len(FormatExperienceRecord(got)) > experienceOutputBudget || !strings.HasSuffix(FormatExperienceRecord(got), "\n") {
			t.Fatalf("record bytes=%d err=%v marshal=%v", len(raw), err, marshalErr)
		}
		original.Error = got.Error
		if !reflect.DeepEqual(got, original) || got.Error == "" {
			t.Fatalf("record facts lost: got=%+v want=%+v", got, original)
		}
	}
	original := ExperienceSearchReply{File: "/current/results/experience.json", Access: "local", Bytes: 36712, Error: strings.Repeat("\x00", 12<<10)}
	got, err := boundExperienceSearchReply(original)
	raw, _ := json.Marshal(got)
	original.Error = got.Error
	if err == nil || got.Error == "" || !reflect.DeepEqual(got, original) || len(raw)+1 > experienceOutputBudget || len(FormatExperienceSearch(got)) > experienceOutputBudget {
		t.Fatalf("metadata facts lost: got=%+v bytes=%d err=%v", got, len(raw), err)
	}
	// The complete semantic text can fit human output yet require a source artifact
	// because of escaped JSON. Bounding must not silently truncate it.
	semantic := ExperienceSearchReply{Text: strings.Repeat("\x00", 6<<10)}
	raw, _ = json.Marshal(semantic)
	if len(raw)+1 <= experienceOutputBudget || len(FormatExperienceSearch(semantic)) > experienceOutputBudget {
		t.Fatal("fixture does not distinguish the budgets")
	}
	got, err = boundExperienceSearchReply(semantic)
	if err == nil || got.Text != semantic.Text || got.File != "" {
		t.Fatal("oversized semantic text was truncated or a file invented")
	}
	// Account for the actual final newline, not just the marshalled object.
	base, _ := json.Marshal(ExperienceSearchReply{Text: ""})
	_ = base
	for _, n := range []int{experienceOutputBudget - len(`{"text":""}`) - 1, experienceOutputBudget - len(`{"text":""}`)} {
		value := ExperienceSearchReply{Text: strings.Repeat("x", n)}
		wire, _ := json.Marshal(value)
		_, err := boundExperienceSearchReply(value)
		if (err == nil) != (len(wire)+1 <= experienceOutputBudget) {
			t.Errorf("JSONNL budget bytes=%d err=%v", len(wire)+1, err)
		}
	}
}
