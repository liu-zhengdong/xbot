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

func TestExperienceRuntimeDualOutputBudgetsPreserveFacts(t *testing.T) {
	confirmed := true
	for _, recorded := range []*bool{nil, &confirmed} {
		original := ExperienceRecordReply{ID: 9223372036854775807, From: -3, To: 700, Recorded: recorded, Error: strings.Repeat("\x00<&>", 12<<10)}
		got, err := boundExperienceRecordReply(original)
		raw, marshalErr := json.Marshal(got)
		if err == nil || marshalErr != nil || len(raw)+1 > TextOutputBudget || len(FormatExperienceRecord(got)) > TextOutputBudget || !strings.HasSuffix(FormatExperienceRecord(got), "\n") {
			t.Fatalf("record bytes=%d err=%v marshal=%v", len(raw), err, marshalErr)
		}
		original.Error = got.Error
		if !reflect.DeepEqual(got, original) || got.Error == "" {
			t.Fatalf("record facts lost: got=%+v want=%+v", got, original)
		}
	}
}
