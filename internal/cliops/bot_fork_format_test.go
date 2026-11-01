package cliops

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBotForkRuntimeFormatPartialFacts(t *testing.T) {
	reply := BotForkReply{Name: "child", Created: true, CreateStage: "config", Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, GroupID: "g_actual", Stage: "handoff_memory", Error: "profile full"}
	want := "name: \"child\"\ncreated: true\n" +
		"creation receipt: original Bot config acknowledged; later setup follows the confirmations below\n" +
		"create_stage: config\nconfirmed: owners, binding, rules\nhandoff_parts: 2\nhandoff_total: 9\n" +
		"handoff_parts counts only successful Remember acknowledgements\n" +
		"uncertain_memory: bot=\"child\" key=\"fork-handoff-3\"\n" +
		"memory outcome unknown: this attempt may already have written Markdown\n" +
		"group_id: g_actual\nstage: handoff_memory\nerror: \"profile full\"\n" +
		"preserve this receipt; do not blindly retry a partial or uncertain fork\n"
	if got := FormatBotFork(reply); got != want {
		t.Fatalf("human receipt=%q want=%q", got, want)
	}
	zero := FormatBotFork(BotForkReply{Stage: "validation"})
	if !strings.Contains(zero, "created: false\n") || !strings.Contains(zero, "handoff_parts: 0\nhandoff_total: 0\n") || strings.Contains(zero, "uncertain_memory:") || strings.Contains(zero, "confirmed:") || !strings.HasSuffix(zero, "\n") || strings.HasSuffix(zero, "\n\n") {
		t.Fatalf("empty receipt=%q", zero)
	}
	complete := FormatBotFork(BotForkReply{Created: true, Confirmed: []string{"owners", "binding", "rules", "handoff", "child_note", "source_note", "group"}, HandoffParts: 2, HandoffTotal: 2, Stage: "complete"})
	if !strings.Contains(complete, "confirmed: owners, binding, rules, handoff, child_note, source_note, group\n") || strings.Contains(complete, "error:") || strings.Contains(complete, "uncertain_memory:") {
		t.Fatalf("complete=%q", complete)
	}
}

func TestBotForkRuntimeDisplayBudgetPreservesFacts(t *testing.T) {
	facts := BotForkReply{Name: "child", Created: true, CreateStage: "persona", Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, GroupID: "g_actual", Stage: "handoff_memory"}
	for _, field := range []string{"name_json", "name_human", "error"} {
		t.Run(field, func(t *testing.T) {
			actual := facts
			switch field {
			case "name_json":
				actual.Name = strings.Repeat("<", 6<<10)
				raw, _ := json.Marshal(actual)
				if len(raw)+1 <= botForkOutputBudget || len(FormatBotFork(actual)) > botForkOutputBudget {
					t.Fatalf("expected JSON-only overflow: json=%d human=%d", len(raw)+1, len(FormatBotFork(actual)))
				}
			case "name_human":
				actual.Name = "x"
				raw, _ := json.Marshal(actual)
				actual.Name = strings.Repeat("x", botForkOutputBudget-len(raw))
				raw, _ = json.Marshal(actual)
				if len(raw)+1 != botForkOutputBudget || len(FormatBotFork(actual)) <= botForkOutputBudget {
					t.Fatalf("expected human-only overflow: json=%d human=%d", len(raw)+1, len(FormatBotFork(actual)))
				}
			case "error":
				actual.Error = strings.Repeat("\x01", 12<<10)
			}
			bounded, err := boundBotForkReply(actual)
			if err == nil || bounded.Error == "" {
				t.Fatalf("overflow succeeded: %+v err=%v", bounded, err)
			}
			want := actual
			want.Name, want.Error = bounded.Name, bounded.Error
			if !reflect.DeepEqual(bounded, want) {
				t.Fatalf("facts lost: got=%+v want=%+v", bounded, want)
			}
			raw, err := json.Marshal(bounded)
			if err != nil || len(raw)+1 > botForkOutputBudget || len(FormatBotFork(bounded)) > botForkOutputBudget {
				t.Fatalf("bounded json=%d human=%d err=%v", len(raw)+1, len(FormatBotFork(bounded)), err)
			}
		})
	}
	if got, err := boundBotForkReply(facts); err != nil || !reflect.DeepEqual(got, facts) {
		t.Fatalf("ordinary receipt changed: %+v %v", got, err)
	}
}
