package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func kindsOf(t *testing.T, tool, args string) []Kind {
	t.Helper()
	need, kinds := Check(tool, json.RawMessage(args))
	if !need && len(kinds) > 0 {
		t.Fatalf("%s returned kinds %v but need=false", tool, kinds)
	}
	if need && len(kinds) == 0 {
		t.Fatalf("%s returned need=true with no kind", tool)
	}
	return kinds
}

func hasKind(kinds []Kind, want Kind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func TestHTTPRequestAlwaysNeedsOwner(t *testing.T) {
	for _, args := range []string{
		`{"url":"https://example.com"}`,
		`{"method":"POST","url":"https://example.com","body":"x"}`,
		`{"url":"https://example.com/?k={{secret:TOKEN}}"}`,
	} {
		if kinds := kindsOf(t, "http_request", args); !hasKind(kinds, KindHTTP) {
			t.Fatalf("%s kinds %v", args, kinds)
		}
	}
}

func TestShellDeleteCommandsFlagDelete(t *testing.T) {
	tests := []string{
		`rm -rf /tmp/x`,
		`sh -c "rm notes.txt"`,
		`cat a | xargs rm`,
		`find . -name '*.tmp' -delete`,
		`python -c "import os; os.remove('x')"`,
		`unlink x`,
	}
	for _, command := range tests {
		args, _ := json.Marshal(map[string]string{"command": command})
		kinds := kindsOf(t, "shell", string(args))
		if !hasKind(kinds, KindDelete) {
			t.Fatalf("%q kinds %v", command, kinds)
		}
	}
}

func TestShellSendCommandsFlagSend(t *testing.T) {
	tests := []string{
		`curl -s https://example.com`,
		`wget https://example.com/x`,
		`nc example.com 80`,
		`echo hi | mail -s x someone@example.com`,
		`python -c "import requests; requests.post('https://x')"`,
	}
	for _, command := range tests {
		args, _ := json.Marshal(map[string]string{"command": command})
		kinds := kindsOf(t, "shell", string(args))
		if !hasKind(kinds, KindSend) {
			t.Fatalf("%q kinds %v", command, kinds)
		}
	}
}

func TestShellReadOnlyInspectionNotFlagged(t *testing.T) {
	for _, command := range []string{`date`, `ls -la`, `git status`, `cat notes.txt`} {
		args, _ := json.Marshal(map[string]string{"command": command})
		need, kinds := Check("shell", args)
		if need || len(kinds) != 0 {
			t.Fatalf("%q need=%v kinds=%v", command, need, kinds)
		}
	}
}

func TestDeleteAndSendTools(t *testing.T) {
	deletes := []string{"delete_file", "remove_file", "delete_bot", "secret_rm", "mcp_remove"}
	for _, name := range deletes {
		if kinds := kindsOf(t, name, `{}`); !hasKind(kinds, KindDelete) {
			t.Fatalf("%s kinds %v", name, kinds)
		}
	}
	sends := []string{"message_bot", "post_group", "send_email", "telegram_send", "lark_send"}
	for _, name := range sends {
		if kinds := kindsOf(t, name, `{}`); !hasKind(kinds, KindSend) {
			t.Fatalf("%s kinds %v", name, kinds)
		}
	}
}

func TestLinksRemoveIsDeletion(t *testing.T) {
	if kinds := kindsOf(t, "links", `{"action":"remove","id":"webhook"}`); !hasKind(kinds, KindDelete) {
		t.Fatalf("remove kinds %v", kinds)
	}
	for _, action := range []string{"search", "load"} {
		if need, _ := Check("links", json.RawMessage(`{"action":"`+action+`","id":"webhook"}`)); need {
			t.Fatalf("%s asked", action)
		}
	}
}

func TestPlanChanges(t *testing.T) {
	cases := []struct {
		tool string
		args string
	}{
		{"write_file", `{"path":"PLAN.md","content":"x"}`},
		{"edit_file", `{"path":"plans/2026.md"}`},
		{"routine_create", `{}`},
		{"routine_update", `{}`},
		{"routine_delete", `{}`},
		{"propose_persona", `{}`},
		{"propose_profile", `{}`},
	}
	for _, c := range cases {
		if kinds := kindsOf(t, c.tool, c.args); !hasKind(kinds, KindPlan) {
			t.Fatalf("%s %s kinds %v", c.tool, c.args, kinds)
		}
	}
	if need, _ := Check("write_file", json.RawMessage(`{"path":"notes.md","content":"x"}`)); need {
		t.Fatal("ordinary write asked")
	}
}

func TestReadOnlyActionsDoNotAsk(t *testing.T) {
	for _, tool := range []string{"read_file", "list_secrets", "list_bots", "list_groups", "routine_list", "recall_history", "expand", "read_skill", "browser_snapshot", "code_show"} {
		if need, kinds := Check(tool, nil); need || len(kinds) != 0 {
			t.Fatalf("%s need=%v kinds=%v", tool, need, kinds)
		}
	}
}

func TestIsPlanPath(t *testing.T) {
	for _, path := range []string{"PLAN.md", "plan", "./plan.txt", "plans/q3.md", "docs/plans/q3.md", "a/PLANS/x", "plan/x.md"} {
		if !IsPlanPath(path) {
			t.Fatalf("%s not a plan", path)
		}
	}
	for _, path := range []string{"notes.md", "planning.md", "README.md"} {
		if IsPlanPath(path) {
			t.Fatalf("%s wrongly a plan", path)
		}
	}
}

func TestSummaryKindsAreDeduplicated(t *testing.T) {
	_, kinds := Check("shell", json.RawMessage(`{"command":"rm x; curl http://x"}`))
	if len(kinds) != 2 {
		t.Fatalf("kinds %v", kinds)
	}
	joined := string(kinds[0]) + string(kinds[1])
	if !strings.Contains(joined, "delete") || !strings.Contains(joined, "send") {
		t.Fatalf("kinds %v", kinds)
	}
}
