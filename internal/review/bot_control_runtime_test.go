package review

import "testing"

func TestBotControlModelPoliciesRetired(t *testing.T) {
	for _, name := range []string{"pin_bot", "set_primary"} {
		if Known(name) {
			t.Errorf("retired native control %s remains a policy entry", name)
		}
	}
}
