package cliops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoutineRuntimeParser(t *testing.T) {
	t.Run("actions and interleaved literal arguments", func(t *testing.T) {
		for _, tc := range []struct {
			argv []string
			want RoutineCommand
		}{
			{[]string{"list", "--json", "--self"}, RoutineCommand{Action: "list", Self: true, JSON: true}},
			{[]string{"create", "--args", `{"id":"inside body"}`, "--self"}, RoutineCommand{Action: "create", Self: true, Args: `{"id":"inside body"}`}},
			{[]string{"update", "--json", "literal ID", "--args-file=-", "--self"}, RoutineCommand{Action: "update", ID: "literal ID", Self: true, JSON: true, ArgsFile: "-"}},
			{[]string{"update", "id", "--self", "--args=not JSON yet"}, RoutineCommand{Action: "update", ID: "id", Self: true, Args: "not JSON yet"}},
			{[]string{"create", "--self", "--args-file", "/nonexistent/request.json"}, RoutineCommand{Action: "create", Self: true, ArgsFile: "/nonexistent/request.json"}},
			{[]string{"delete", "--self", "id", "--json"}, RoutineCommand{Action: "delete", ID: "id", Self: true, JSON: true}},
		} {
			got, err := ParseRoutineRuntime(tc.argv)
			if err != nil || got != tc.want {
				t.Errorf("argv=%q command=%+v want=%+v error=%v", tc.argv, got, tc.want, err)
			}
		}
	})
	t.Run("help requires no identity or body IO", func(t *testing.T) {
		for _, argv := range [][]string{
			{"help"}, {"--help"}, {"-h"}, {"list", "--help"}, {"create", "-h"}, {"update", "--help"}, {"delete", "--help"},
			{"create", "--help", "--args-file", "/nonexistent/request.json"},
		} {
			if got, err := ParseRoutineRuntime(argv); err != nil || !got.Help {
				t.Errorf("argv=%q command=%+v error=%v", argv, got, err)
			}
		}
	})
	t.Run("reject ambiguous unsupported or mismatched syntax", func(t *testing.T) {
		for _, argv := range [][]string{
			nil,
			{"show", "--self"}, {"migrate", "--self"},
			{"list"}, {"create", "--args", "{}"}, {"update", "id", "--args", "{}"}, {"delete", "id"},
			{"list", "id", "--self"}, {"create", "id", "--self", "--args", "{}"},
			{"update", "--self", "--args", "{}"}, {"update", "", "--self", "--args", "{}"},
			{"update", "id", "other", "--self", "--args", "{}"},
			{"delete", "--self"}, {"delete", "", "--self"}, {"delete", "id", "other", "--self"},
			{"create", "--self"}, {"update", "id", "--self"},
			{"create", "--self", "--args"}, {"update", "id", "--self", "--args-file"},
			{"create", "--self", "--args="}, {"create", "--self", "--args-file", ""},
			{"create", "--self", "--args", "{}", "--args-file", "-"},
			{"update", "id", "--self", "--args-file", "a", "--args-file=b"},
			{"create", "--self", "--args", "{}", "--args={}"},
			{"list", "--self", "--args", "{}"}, {"delete", "id", "--self", "--args-file", "-"},
			{"list", "--self", "--self"}, {"list", "--self", "--json", "--json"},
			{"list", "--self=true"}, {"list", "--self", "--json=false"}, {"list", "--help=true"},
			{"list", "--self", "--bot", "ada"}, {"update", "id", "--self", "--skills", "ship"},
			{"list", "--self", "--unknown"}, {"list", "--help", "--unknown"},
			{"list", "--help", "-h"}, {"help", "--self"},
			{"create", "--help", "--args={}", "--args-file=-"},
		} {
			if got, err := ParseRoutineRuntime(argv); err == nil {
				t.Errorf("accepted argv=%q command=%+v", argv, got)
			}
		}
	})
}

func TestRoutineListReplyEmptyAndArtifact(t *testing.T) {
	empty := []RoutineView{}
	inline, err := json.Marshal(RoutineListReply{Routines: &empty})
	if err != nil || !strings.Contains(string(inline), `"routines":[]`) {
		t.Fatalf("empty inline list=%s error=%v", inline, err)
	}
	artifact, err := json.Marshal(RoutineListReply{Count: 3, File: "results/list.json", Access: "local", Bytes: 123})
	if err != nil || strings.Contains(string(artifact), `"routines"`) {
		t.Fatalf("artifact list=%s error=%v", artifact, err)
	}
	var restored RoutineListReply
	if err := json.Unmarshal(artifact, &restored); err != nil || restored.Routines != nil || restored.Count != 3 || restored.File != "results/list.json" || restored.Access != "local" || restored.Bytes != 123 {
		t.Fatalf("restored=%+v error=%v", restored, err)
	}
}
