package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/link"
)

func TestRoutineToolsPrecheck(t *testing.T) {
	b := testBot(t, link.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}}
	ctx := context.Background()
	if _, err := (routineCreate{k}).Run(ctx, json.RawMessage(`{"id":"check","name":"check","prompt":"p","cron":"0 * * * *","precheck":"false"}`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"id":"check","name":"renamed"}`, "false"},
		{`{"id":"check","precheck":"true"}`, "true"},
		{`{"id":"check","precheck":""}`, ""},
	} {
		if _, err := (routineUpdate{k}).Run(ctx, json.RawMessage(tc.args)); err != nil {
			t.Fatal(err)
		}
		r, err := tm.Load("check")
		if err != nil || r.Precheck != tc.want {
			t.Fatalf("precheck %q, want %q: %v", r.Precheck, tc.want, err)
		}
		out, err := (routineList{k}).Run(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tc.want != "" && !strings.Contains(out, `precheck "`+tc.want+`"`) {
			t.Fatalf("list: %s", out)
		}
	}
	if _, err := (routineUpdate{k}).Run(ctx, json.RawMessage(`{"id":"check","precheck":false}`)); err == nil {
		t.Fatal("accepted boolean")
	}
}
