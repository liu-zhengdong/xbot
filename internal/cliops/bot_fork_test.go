package cliops

import (
	"encoding/json"
	"testing"
)

func TestBotForkRuntimeDTO(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{BotFork{Name: "child", Role: "duties"}, `{"name":"child","role":"duties"}`},
		{BotFork{Name: "child", Role: "duties", Group: "project"}, `{"name":"child","role":"duties","group":"project"}`},
		{BotForkReply{}, `{"created":false,"handoff_parts":0,"handoff_total":0,"stage":""}`},
		{BotForkReply{Name: "child", Created: true, CreateStage: "persona", Confirmed: []string{"owners", "binding", "rules"}, HandoffParts: 2, HandoffTotal: 9, UncertainMemory: &ForkMemoryAttempt{Bot: "child", Key: "fork-handoff-3"}, GroupID: "g_actual", Stage: "handoff_memory", Error: "profile full"}, `{"name":"child","created":true,"create_stage":"persona","confirmed":["owners","binding","rules"],"handoff_parts":2,"handoff_total":9,"uncertain_memory":{"bot":"child","key":"fork-handoff-3"},"group_id":"g_actual","stage":"handoff_memory","error":"profile full"}`},
	} {
		raw, err := json.Marshal(tc.value)
		if err != nil || string(raw) != tc.want {
			t.Fatalf("wire=%s want=%s err=%v", raw, tc.want, err)
		}
	}
}
