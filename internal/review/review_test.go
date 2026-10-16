package review

import (
	"encoding/json"
	"testing"
)

func need(t *testing.T, tool, args string) bool {
	t.Helper()
	return NeedsOwner(tool, json.RawMessage(args))
}

func shellNeed(t *testing.T, command string) bool {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"command": command})
	return NeedsOwner("shell", args)
}

func TestTableHasEveryClass(t *testing.T) {
	want := map[string]Class{
		"read_file":       Readonly,
		"list_secrets":    Readonly,
		"recall_history":  Readonly,
		"routine_list":    Readonly,
		"code_show":       Readonly,
		"remember":        Normal,
		"forget":          Normal,
		"recall":          Readonly,
		"delete_bot":      Delete,
		"secret_rm":       Delete,
		"telegram_send":   ExternalSend,
		"lark_send":       ExternalSend,
		"http_request":    HTTP,
		"shell":           Shell,
		"node_call":       Shell,
		"write_file":      SelfEdit,
		"edit_file":       SelfEdit,
		"propose_persona": SelfEdit,
		"propose_profile": SelfEdit,
		"routine_create":  Normal,
		"routine_update":  Normal,
		"routine_delete":  Normal,
		"message_bot":     Normal,
		"post_group":      Normal,
		"write_skill":     Normal,
		"code_start":      Normal,
		"browser_stop":    Normal,
	}
	for name, class := range want {
		if !Known(name) {
			t.Fatalf("%s is not in the table", name)
		}
		if got := ClassOf(name); got != class {
			t.Fatalf("%s class %q want %q", name, got, class)
		}
	}
}

func TestHTTPNeedsOwnerOnlyForSecretOrWriteMethod(t *testing.T) {
	ask := []string{
		`{"method":"POST","url":"https://example.com","body":"x"}`,
		`{"method":"PUT","url":"https://example.com","body":"x"}`,
		`{"method":"HEAD","url":"https://example.com/?k={{secret:TOKEN}}"}`,
		`{"url":"https://example.com/?k={{secret:TOKEN}}"}`,
		`{"url":"https://example.com","headers":{"Authorization":"Bearer {{secret:TOKEN}}"}}`,
	}
	for _, args := range ask {
		if !need(t, "http_request", args) {
			t.Fatalf("%s did not ask", args)
		}
	}
	for _, args := range []string{
		`{"url":"https://example.com"}`,
		`{"method":"GET","url":"https://example.com"}`,
		`{"method":"head","url":"https://example.com"}`,
	} {
		if need(t, "http_request", args) {
			t.Fatalf("%s asked", args)
		}
	}
}

func TestShellNeedsOwnerForDeleteSendAndKeys(t *testing.T) {
	for _, command := range []string{
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
	} {
		if !shellNeed(t, command) {
			t.Fatalf("%q did not ask", command)
		}
	}
	if !need(t, "shell", `{"command":"printenv X","keys":true}`) {
		t.Fatal("keys:true did not ask")
	}
}

func TestShellPlainAndReadOnlyActionsDoNotAsk(t *testing.T) {
	for _, command := range []string{
		`date`, `ls -la`, `git status`, `cat notes.txt`, `python train.py`, `go test ./...`,
		`curl -s https://example.com`,
		`curl -sS -m 1 -o /dev/null http://127.0.0.1:9/report`,
		`curl -I https://example.com`,
		`wget https://example.com/x`,
		`scp notes.txt backup.txt`,
		`rsync -av a/ b/`,
		`python -c "import requests; requests.get('https://x')"`,
		`git log --grep clean`,
		`find . -name '*.tmp'`,
	} {
		if shellNeed(t, command) {
			t.Fatalf("%q wrongly asked", command)
		}
	}
}

func TestDeleteToolsAsk(t *testing.T) {
	for _, name := range []string{"delete_file", "remove_file", "delete_bot", "secret_rm", "secret_delete", "mcp_remove", "delete_link"} {
		if !need(t, name, `{}`) {
			t.Fatalf("%s did not ask", name)
		}
	}
}

func TestExternalSendToolsAsk(t *testing.T) {
	for _, name := range []string{"send_email", "telegram_send", "lark_send", "send_webhook"} {
		if !need(t, name, `{}`) {
			t.Fatalf("%s did not ask", name)
		}
	}
}

func TestBotToBotAndRoutinesAreNormal(t *testing.T) {
	for _, name := range []string{"message_bot", "post_group", "routine_create", "routine_update", "routine_delete", "routine_pause"} {
		if need(t, name, `{"bot":"bob","text":"hi","id":"r1"}`) {
			t.Fatalf("%s asked", name)
		}
	}
}

func TestLinksInstallAndRemoveAsk(t *testing.T) {
	if !need(t, "links", `{"action":"install","id":"webhook"}`) {
		t.Fatal("install did not ask")
	}
	if !need(t, "links", `{"action":"remove","id":"webhook"}`) {
		t.Fatal("remove did not ask")
	}
	for _, action := range []string{"search", "load"} {
		if need(t, "links", `{"action":"`+action+`","id":"webhook"}`) {
			t.Fatalf("%s asked", action)
		}
	}
}

func TestSelfEditAndReadOnlyClasses(t *testing.T) {
	for _, name := range []string{"propose_persona", "propose_profile"} {
		if !need(t, name, `{}`) {
			t.Fatalf("%s did not ask", name)
		}
	}
	// write_file/edit_file are path-dependent; the kernel checks the path.
	if need(t, "write_file", `{"path":"notes.md","content":"x"}`) {
		t.Fatal("write_file asked without a path check")
	}
	if need(t, "edit_file", `{"path":"plans/2026.md"}`) {
		t.Fatal("edit_file asked without a path check")
	}
	for _, name := range []string{"read_file", "list_secrets", "list_bots", "list_groups", "routine_list", "recall_history", "expand", "read_skill", "read_subagent", "expand_subagent", "browser_snapshot", "code_show", "node_list"} {
		if need(t, name, `{}`) {
			t.Fatalf("%s asked", name)
		}
	}
}

func TestMCPAndUnknownTools(t *testing.T) {
	if !Known("mcp_docs_echo") {
		t.Fatal("mcp tool not known")
	}
	if ClassOf("mcp_docs_echo") != ExternalSend {
		t.Fatalf("mcp class %q", ClassOf("mcp_docs_echo"))
	}
	if !need(t, "mcp_docs_echo", `{}`) {
		t.Fatal("mcp tool did not ask")
	}
	if Known("mystery_tool") {
		t.Fatal("unknown tool reported known")
	}
	if ClassOf("mystery_tool") != Normal {
		t.Fatalf("unknown class %q", ClassOf("mystery_tool"))
	}
}

func TestMachineTouchingSet(t *testing.T) {
	for _, name := range []string{"shell", "read_file", "write_file", "edit_file", "desktop_screenshot", "browser_open", "browser_click"} {
		if !MachineTouching(name) {
			t.Fatalf("%s not machine-touching", name)
		}
	}
	for _, name := range []string{"browser_stop", "message_bot", "http_request", "node_call"} {
		if MachineTouching(name) {
			t.Fatalf("%s wrongly machine-touching", name)
		}
	}
}

func TestSummaryKindsAreDeduplicated(t *testing.T) {
	deletes, sends := ShellFlags(`rm x; cat a | curl -d y https://x`)
	if !deletes || !sends {
		t.Fatalf("deletes=%v sends=%v", deletes, sends)
	}
}
