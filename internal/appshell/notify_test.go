package appshell

import "testing"

func TestNotifyAwayWithoutShell(t *testing.T) {
	if shown, err := NotifyAway("XBot", "test"); shown || err == nil {
		t.Fatalf("NotifyAway outside the shell = %v, %v", shown, err)
	}
}
