package review

import (
	"encoding/json"
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

func shellKinds(t *testing.T, command string) []Kind {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"command": command})
	return kindsOf(t, "shell", string(args))
}

func TestHTTPRequestNeedsOwnerOnlyForSecretOrWriteMethod(t *testing.T) {
	ask := []string{
		`{"method":"POST","url":"https://example.com","body":"x"}`,
		`{"method":"PUT","url":"https://example.com","body":"x"}`,
		`{"method":"HEAD","url":"https://example.com/?k={{secret:TOKEN}}"}`,
		`{"url":"https://example.com/?k={{secret:TOKEN}}"}`,
		`{"url":"https://example.com","headers":{"Authorization":"Bearer {{secret:TOKEN}}"}}`,
	}
	for _, args := range ask {
		if kinds := kindsOf(t, "http_request", args); !hasKind(kinds, KindHTTP) {
			t.Fatalf("%s kinds %v", args, kinds)
		}
	}
	for _, args := range []string{
		`{"url":"https://example.com"}`,
		`{"method":"GET","url":"https://example.com"}`,
		`{"method":"head","url":"https://example.com"}`,
	} {
		if need, kinds := Check("http_request", json.RawMessage(args)); need || len(kinds) != 0 {
			t.Fatalf("%s need=%v kinds=%v", args, need, kinds)
		}
	}
}

func TestShellDeleteCommandsFlagDelete(t *testing.T) {
	tests := []string{
		`rm -rf /tmp/x`,
		`sh -c "rm notes.txt"`,
		`cat a | xargs rm`,
		`rmdir empty`,
		`unlink x`,
		`shred -u x`,
		`find . -name '*.tmp' -delete`,
		`git clean -fdx`,
		`git -C /repo clean -fd`,
		`python -c "import os; os.remove('x')"`,
		`python -c "import shutil; shutil.rmtree('x')"`,
	}
	for _, command := range tests {
		if kinds := shellKinds(t, command); !hasKind(kinds, KindDelete) {
			t.Fatalf("%q kinds %v", command, kinds)
		}
	}
	for _, command := range []string{`git status`, `git log --grep clean`, `find . -name '*.tmp'`} {
		if kinds := shellKinds(t, command); hasKind(kinds, KindDelete) {
			t.Fatalf("%q wrongly flagged delete: %v", command, kinds)
		}
	}
}

func TestShellSendCommandsFlagSend(t *testing.T) {
	tests := []string{
		`curl -X POST https://example.com`,
		`curl -XPOST https://example.com`,
		`curl -d 'a=1' https://example.com`,
		`curl --data-binary @file https://example.com`,
		`curl -F file=@x https://example.com`,
		`curl -T notes.txt ssh://host/x`,
		`curl --request PUT https://example.com`,
		`wget --post-data 'a=1' https://example.com`,
		`wget --method=POST https://example.com`,
		`nc example.com 80`,
		`echo hi | mail -s x someone@example.com`,
		`sendmail -t < msg.txt`,
		`scp notes.txt user@host:/tmp/`,
		`rsync -av ./out host:/srv/out`,
		`python -c "import requests; requests.post('https://x')"`,
	}
	for _, command := range tests {
		if kinds := shellKinds(t, command); !hasKind(kinds, KindSend) {
			t.Fatalf("%q kinds %v", command, kinds)
		}
	}
}

func TestShellPlainReadsAreNotSends(t *testing.T) {
	for _, command := range []string{
		`curl -s https://example.com`,
		`curl -sS -m 1 -o /dev/null http://127.0.0.1:9/report`,
		`curl -I https://example.com`,
		`wget https://example.com/x`,
		`scp notes.txt backup.txt`,
		`rsync -av a/ b/`,
		`python -c "import requests; requests.get('https://x')"`,
	} {
		if kinds := shellKinds(t, command); hasKind(kinds, KindSend) {
			t.Fatalf("%q wrongly flagged send: %v", command, kinds)
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

func TestDeleteTools(t *testing.T) {
	for _, name := range []string{"delete_file", "remove_file", "delete_bot", "secret_rm", "secret_delete", "mcp_remove", "delete_link"} {
		if kinds := kindsOf(t, name, `{}`); !hasKind(kinds, KindDelete) {
			t.Fatalf("%s kinds %v", name, kinds)
		}
	}
}

func TestExternalSendTools(t *testing.T) {
	for _, name := range []string{"send_email", "telegram_send", "lark_send", "send_webhook"} {
		if kinds := kindsOf(t, name, `{}`); !hasKind(kinds, KindSend) {
			t.Fatalf("%s kinds %v", name, kinds)
		}
	}
}

func TestBotToBotToolsDoNotAsk(t *testing.T) {
	for _, name := range []string{"message_bot", "post_group"} {
		if need, kinds := Check(name, json.RawMessage(`{"bot":"bob","text":"hi"}`)); need || len(kinds) != 0 {
			t.Fatalf("%s need=%v kinds=%v", name, need, kinds)
		}
	}
}

func TestPlanRoutinePersonaNotClassified(t *testing.T) {
	cases := []struct {
		tool string
		args string
	}{
		{"write_file", `{"path":"PLAN.md","content":"x"}`},
		{"edit_file", `{"path":"plans/2026.md"}`},
		{"write_file", `{"path":"notes.md","content":"x"}`},
		{"routine_create", `{}`},
		{"routine_update", `{"id":"r1","enabled":false}`},
		{"routine_delete", `{"id":"r1"}`},
		{"routine_pause", `{"id":"r1"}`},
		{"propose_persona", `{}`},
		{"propose_profile", `{}`},
	}
	for _, c := range cases {
		if need, kinds := Check(c.tool, json.RawMessage(c.args)); need || len(kinds) != 0 {
			t.Fatalf("%s %s need=%v kinds=%v", c.tool, c.args, need, kinds)
		}
	}
}

func TestRoutineDeleteIsNotDataDelete(t *testing.T) {
	args := json.RawMessage(`{"id":"r1"}`)
	if need, kinds := Check("routine_delete", args); need || hasKind(kinds, KindDelete) {
		t.Fatalf("routine_delete need=%v kinds=%v", need, kinds)
	}
	if need, kinds := Check("routine_remove", args); need || hasKind(kinds, KindDelete) {
		t.Fatalf("routine_remove need=%v kinds=%v", need, kinds)
	}
}

func TestLinksRemoveIsDeletion(t *testing.T) {
	if kinds := kindsOf(t, "links", `{"action":"remove","id":"webhook"}`); !hasKind(kinds, KindDelete) {
		t.Fatalf("remove kinds %v", kinds)
	}
	for _, action := range []string{"search", "load", "install"} {
		if need, _ := Check("links", json.RawMessage(`{"action":"`+action+`","id":"webhook"}`)); need {
			t.Fatalf("%s asked", action)
		}
	}
}

func TestReadOnlyActionsDoNotAsk(t *testing.T) {
	for _, tool := range []string{"read_file", "list_secrets", "list_bots", "list_groups", "routine_list", "recall_history", "expand", "read_skill", "browser_snapshot", "code_show"} {
		if need, kinds := Check(tool, nil); need || len(kinds) != 0 {
			t.Fatalf("%s need=%v kinds=%v", tool, need, kinds)
		}
	}
}

func TestSummaryKindsAreDeduplicated(t *testing.T) {
	kinds := shellKinds(t, `rm x; cat a | curl -d y https://x`)
	if len(kinds) != 2 {
		t.Fatalf("kinds %v", kinds)
	}
	if !hasKind(kinds, KindDelete) || !hasKind(kinds, KindSend) {
		t.Fatalf("kinds %v", kinds)
	}
}
