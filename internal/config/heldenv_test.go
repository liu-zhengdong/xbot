package config

import "testing"

func TestHeldEnvRestoresKeysForReexec(t *testing.T) {
	t.Setenv("XBOT_TEST_HELD", "v1")
	HoldKeys([]string{"XBOT_TEST_HELD"})
	defer ForgetKeys()
	env := HeldEnv([]string{"A=1", "XBOT_TEST_HELD="})
	got := map[string]int{}
	for _, kv := range env {
		got[kv]++
	}
	if got["A=1"] != 1 || got["XBOT_TEST_HELD=v1"] != 1 || got["XBOT_TEST_HELD="] != 0 {
		t.Fatalf("env %v", env)
	}
}
