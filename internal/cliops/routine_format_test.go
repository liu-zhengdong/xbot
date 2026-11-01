package cliops

import "testing"

func TestFormatRoutineList(t *testing.T) {
	empty := []RoutineView{}
	views := []RoutineView{{
		ID: "once", Name: "name", Prompt: "first\nsecond", Precheck: "test -f ready", Skills: []string{"ship", "ship"},
		Cron: "0 9 * * *", At: "literal", Enabled: false, LastSlot: "slot", Zone: "local",
	}}
	for _, tc := range []struct {
		name  string
		reply RoutineListReply
		want  string
	}{
		{"empty", RoutineListReply{Routines: &empty}, "count: 0\n"},
		{"complete inline fields", RoutineListReply{Routines: &views, Count: 1}, "count: 1\nid: once\nname: name\nprompt: first\nsecond\nprecheck: test -f ready\nskills: [\"ship\",\"ship\"]\ncron: 0 9 * * *\nat: literal\nenabled: false\nlast_slot: slot\nzone: local\n"},
		{"artifact and error", RoutineListReply{Count: 300, File: "results/full.json", Access: "local", Bytes: 28000, Error: "read failed"}, "count: 300\nresults/full.json (access: local, bytes: 28000)\nerror: read failed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatRoutineList(tc.reply); got != tc.want {
				t.Fatalf("formatted output changed:\ngot  %q\nwant %q", got, tc.want)
			}
		})
	}
}
