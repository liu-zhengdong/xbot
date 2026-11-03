package kernel

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
)

type boundReadOnlyInterpreter struct{ calls int }

func (*boundReadOnlyInterpreter) Name() string        { return "sh" }
func (*boundReadOnlyInterpreter) Description() string { return "fixture interpreter" }
func (*boundReadOnlyInterpreter) Parameters() any     { return map[string]any{"type": "object"} }
func (*boundReadOnlyInterpreter) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (s *boundReadOnlyInterpreter) Run(context.Context, json.RawMessage) (string, error) {
	s.calls++
	return "accepted", nil
}

func TestResearchRoutineDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" routine list --self --json`, `"$XBOT_BIN" routine --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" routine create --self --args '{}'`,
		`"$XBOT_BIN" routine update r --self --args '{}'`,
		`"$XBOT_BIN" routine delete r --self`,
		`"$XBOT_BIN" routine list --bot other`,
		`"$XBOT_BIN" routine migrate --dry-run`,
		`"$XBOT_BIN" --root /other routine list --self`,
		`"$XBOT_BIN" routine list --self; echo injected`,
		`xbot routine list --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other source accepted %s", command)
		}
	}
	if interpreter.calls != 2 {
		t.Fatalf("rejected command reached interpreter: %d", interpreter.calls)
	}
}

func TestResearchSkillDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" skill read ship --self --json`, `"$XBOT_BIN" skill --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" skill write ship --description d --body body --self`,
		`"$XBOT_BIN" skill show ship`,
		`"$XBOT_BIN" --root /other skill read ship --self`,
		`"$XBOT_BIN" skill read ship --self; echo injected`,
		`xbot skill read ship --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other-target accepted %s", command)
		}
	}
	if interpreter.calls != 2 {
		t.Fatalf("rejected command reached interpreter before any CLI RPC: calls=%d", interpreter.calls)
	}
}

func TestResearchMCPDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{
		`"$XBOT_BIN" mcp list --self --json`,
		`"$XBOT_BIN" mcp tools docs --self`,
		`"$XBOT_BIN" mcp describe docs 'remote/name' --self`,
		`"$XBOT_BIN" mcp call docs query --self --readonly --args '{"n":9007199254740993}'`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	if interpreter.calls != 4 {
		t.Fatal("read did not reach original interpreter")
	}
	for _, command := range []string{
		`"$XBOT_BIN" mcp call docs query --self`,
		`"$XBOT_BIN" mcp add docs --url http://127.0.0.1:9 --self`,
		`"$XBOT_BIN" mcp list --http 127.0.0.1:9 --bot other`,
		`"$XBOT_BIN" mcp offline list other`,
		`"$XBOT_BIN" --root /other mcp list --self`,
		`"$XBOT_BIN" mcp list --self; echo injected`,
		`xbot mcp list --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other-target accepted %s", command)
		}
	}
	if interpreter.calls != 4 {
		t.Fatal("rejected command reached interpreter")
	}
}

func TestResearchCompactHelpOnlyUsesBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	k.operations.mu.Lock()
	binary, root := k.operations.binary, k.operations.root
	k.operations.mu.Unlock()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, test := range []struct {
		name, command string
		allowed       bool
	}{
		{"help", `"$XBOT_BIN" compact --help`, true},
		{"self_help", `"$XBOT_BIN" compact --self --help`, true},
		{"range_help", `"$XBOT_BIN" compact --self --from 1 --to 2 --help`, true},
		{"canonical_binary", strconv.Quote(binary) + ` compact --help`, true},
		{"bound_root", `"$XBOT_BIN" --root ` + strconv.Quote(root) + ` compact --help`, true},
		{"bound_root_equals", `"$XBOT_BIN" ` + strconv.Quote("--root="+root) + ` compact --help`, true},
		{"self", `"$XBOT_BIN" compact --self --from 1 --to 2`, false},
		{"focus_help_data", `"$XBOT_BIN" compact --self --from 1 --to 2 --focus --help`, false},
		{"focus_file_help_data", `"$XBOT_BIN" compact --self --from 1 --to 2 --focus-file --help`, false},
		{"focus_equals_help_data", `"$XBOT_BIN" compact --self --from 1 --to 2 --focus=--help`, false},
		{"wrong_root", `"$XBOT_BIN" --root /other compact --help`, false},
		{"wrong_binary", `/other/xbot compact --help`, false},
		{"bare_binary", `xbot compact --help`, false},
		{"chaining", `"$XBOT_BIN" compact --help; echo injected`, false},
		{"substitution", `"$XBOT_BIN" compact --focus "$(echo --help)"`, false},
		{"repeated_help", `"$XBOT_BIN" compact --help --help`, false},
		{"unknown_flag", `"$XBOT_BIN" compact --help --query text`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := interpreter.calls
			raw, _ := json.Marshal(map[string]string{"command": test.command})
			_, err := shell.Run(context.Background(), raw)
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v command=%s: %v", test.allowed, test.command, err)
			}
			wantCalls := before
			if test.allowed {
				wantCalls++
			}
			if interpreter.calls != wantCalls {
				t.Fatalf("interpreter calls=%d, want %d", interpreter.calls, wantCalls)
			}
		})
	}
	k.CloseOperations()
	before := interpreter.calls
	if _, err := shell.Run(context.Background(), json.RawMessage(`{"command":"\"$XBOT_BIN\" compact --help"}`)); err == nil || interpreter.calls != before {
		t.Fatal("unbound compact help reached interpreter")
	}
}

func TestResearchLinksReadOnlyUsesBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	k.operations.mu.Lock()
	binary, root := k.operations.binary, k.operations.root
	k.operations.mu.Unlock()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, test := range []struct {
		name, command string
		allowed       bool
	}{
		{"help", `"$XBOT_BIN" links --help`, true},
		{"search_help", `"$XBOT_BIN" links search --help`, true},
		{"load_help", `"$XBOT_BIN" links load --help`, true},
		{"search_self", `"$XBOT_BIN" links search --self`, true},
		{"search_self_query", `"$XBOT_BIN" links search --self --query browser`, true},
		{"search_self_query_file", `"$XBOT_BIN" links search --self --query-file catalog.txt`, true},
		{"search_self_json", `"$XBOT_BIN" links search --self --json`, true},
		{"canonical_binary", strconv.Quote(binary) + ` links search --self`, true},
		{"bound_root", `"$XBOT_BIN" --root ` + strconv.Quote(root) + ` links search --self`, true},
		{"bound_root_equals", `"$XBOT_BIN" ` + strconv.Quote("--root="+root) + ` links search --self`, true},
		{"load_self", `"$XBOT_BIN" links load --self browser`, false},
		{"load_self_json", `"$XBOT_BIN" links load --self telegram --json`, false},
		{"search_no_self", `"$XBOT_BIN" links search`, false},
		{"search_query_data", `"$XBOT_BIN" links search --self --query "--help"`, true},
		{"search_query_file_data", `"$XBOT_BIN" links search --self --query-file "--help"`, true},
		{"install_shape", `"$XBOT_BIN" links install --self browser`, false},
		{"remove_shape", `"$XBOT_BIN" links remove --self browser`, false},
		{"wrong_root", `"$XBOT_BIN" --root /other links search --self`, false},
		{"wrong_binary", `/other/xbot links search --self`, false},
		{"bare_binary", `xbot links search --self`, false},
		{"chaining", `"$XBOT_BIN" links search --self; echo injected`, false},
		{"substitution", `"$XBOT_BIN" links search --self --query "$(echo browser)"`, false},
		{"repeated_help", `"$XBOT_BIN" links --help --help`, false},
		{"unknown_flag", `"$XBOT_BIN" links search --self --focus x`, false},
		{"extra_positional", `"$XBOT_BIN" links load --self browser extra`, false},
		{"load_no_id", `"$XBOT_BIN" links load --self`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := interpreter.calls
			raw, _ := json.Marshal(map[string]string{"command": test.command})
			_, err := shell.Run(context.Background(), raw)
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v command=%s: %v", test.allowed, test.command, err)
			}
			wantCalls := before
			if test.allowed {
				wantCalls++
			}
			if interpreter.calls != wantCalls {
				t.Fatalf("interpreter calls=%d, want %d", interpreter.calls, wantCalls)
			}
		})
	}
	k.CloseOperations()
	before := interpreter.calls
	if _, err := shell.Run(context.Background(), json.RawMessage(`{"command":"\"$XBOT_BIN\" links search --self"}`)); err == nil || interpreter.calls != before {
		t.Fatal("unbound links search reached interpreter")
	}
}
