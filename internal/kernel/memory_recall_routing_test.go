package kernel

import (
	"encoding/json"
	"testing"
)

func TestMemoryRecallRuntimeReadonlyAndLoopBudget(t *testing.T) {
	k, _ := memoryRecallRuntimeFixture(t)
	cases := []struct {
		command string
		allowed bool
	}{
		{`"$XBOT_BIN" memory-recall --self --query 'needle ; quoted | symbols'`, true},
		{`"$XBOT_BIN" memory-recall --self`, true},
		{`"$XBOT_BIN" memory-recall --help`, true},
		{`"$XBOT_BIN" memory-recall --query --help`, false},
		{`"$XBOT_BIN" memory-recall --self --query`, false},
		{`"$XBOT_BIN" memory-recall --self --output-file target`, false},
		{`"$XBOT_BIN" memory-recall --self ; echo unrelated`, false},
		{`echo 'memory-recall --self --help'`, false},
		{`"$XBOT_BIN" experience record --self --summary note`, false},
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
			if tc.allowed {
				want = looseThreshold
			}
			if got := callThreshold("sh\n" + string(raw)); got != want {
				t.Errorf("actual Shell call loop budget got %d, want %d", got, want)
			}
		})
	}
}
