package kernel

import (
	"context"
	"encoding/json"
	"testing"

	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestRoutinePersistedPrecheck(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	// This unit checks persisted precheck values. Source approval is exercised
	// through the real runtime call in routine_review_test.go.
	if err := tm.Save(link.Routine{ID: "check", Name: "check", Prompt: "p", Cron: "0 * * * *", Precheck: "false", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	command, empty := "true", ""
	for _, tc := range []struct {
		name     string
		precheck *string
		want     string
	}{
		{name: "renamed", want: "false"},
		{precheck: &command, want: "true"},
		{precheck: &empty},
	} {
		if err := tm.Update("check", func(r *link.Routine) error {
			if tc.name != "" {
				r.Name = tc.name
			}
			if tc.precheck != nil {
				r.Precheck = *tc.precheck
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		r, err := tm.Load("check")
		if err != nil || r.Precheck != tc.want || r.Name != "renamed" {
			t.Fatalf("routine %+v, want precheck %q and retained name: %v", r, tc.want, err)
		}
		out, err := k.RoutineHandlers().List(context.Background())
		if err != nil || out.Error != "" || out.Routines == nil || len(*out.Routines) != 1 || out.Count != 1 {
			t.Fatalf("list %+v: %v", out, err)
		}
		if got := (*out.Routines)[0].Precheck; got != tc.want {
			t.Fatalf("listed precheck %q, want %q", got, tc.want)
		}
	}
	var patch cliops.RoutinePatch
	if err := json.Unmarshal([]byte(`{"precheck":false}`), &patch); err == nil {
		t.Fatal("accepted boolean precheck")
	}
}
