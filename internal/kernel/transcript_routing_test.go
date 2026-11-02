package kernel

import (
	"encoding/json"
	"testing"
)

func TestTranscriptRuntimeReadonlyAndOriginalSearchLoopBudget(t *testing.T) {
	k, _ := memoryRecallRuntimeFixture(t)
	cases := []struct {
		command string
		allowed bool
		loose   bool
	}{
		{`"$XBOT_BIN" transcript search --self --query 'needle ; quoted | data'`, true, true},
		{`"$XBOT_BIN" transcript search --self`, true, true},
		{`"$XBOT_BIN" transcript search --help`, true, true},
		{`"$XBOT_BIN" transcript --help`, true, false},
		{`"$XBOT_BIN" transcript expand --self --from 0 --to -1`, true, false},
		{`"$XBOT_BIN" transcript expand --help`, true, false},
		{`"$XBOT_BIN" transcript detail --self --seq 0 --experience 1`, true, false},
		{`"$XBOT_BIN" transcript detail --self --seq 1 --experience -1`, true, false},
		{`"$XBOT_BIN" transcript detail --help`, true, false},
		{`"$XBOT_BIN" transcript search --query --help`, false, false},
		{`"$XBOT_BIN" transcript detail --self --seq 1 --bot foreign`, false, false},
		{`"$XBOT_BIN" transcript search --self ; echo unrelated`, false, false},
		{`"$XBOT_BIN" transcript expand --self --from 1 --to 1 | cat`, false, false},
		{`echo 'transcript search --self --help'`, false, false},
		{`"$XBOT_BIN" experience search --self`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"command": tc.command})
			if err != nil {
				t.Fatal(err)
			}
			if got := k.allowReadOnlyPlatform("sh", raw); got != tc.allowed {
				t.Errorf("research classification got %v, want %v", got, tc.allowed)
			}
			want := loopThreshold
			if tc.loose {
				want = looseThreshold
			}
			if got := callThreshold("sh\n" + string(raw)); got != want {
				t.Errorf("actual Shell loop threshold got %d, want %d", got, want)
			}
		})
	}
}
