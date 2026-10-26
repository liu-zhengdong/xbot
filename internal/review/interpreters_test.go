package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInterpreterPolicyParity(t *testing.T) {
	for _, name := range []string{"bash", "sh", "zsh", "powershell", "cmd", "git-bash"} {
		t.Run(name, func(t *testing.T) {
			if ClassOf(name) != Shell || !MachineTouching(name) {
				t.Fatal("missing policy")
			}
			for _, raw := range []string{`{"command":"rm file","description":"Inspect files"}`, `{"command":"echo ok","keys":true}`} {
				if !NeedsOwner(name, json.RawMessage(raw)) {
					t.Fatalf("missed review %s", raw)
				}
			}
			raw := json.RawMessage(`{"command":"echo x > file"}`)
			if resource, ok := ResourceOf(name, raw, "/work"); !ok || resource != "dir:/work" {
				t.Fatalf("missing resource %q %v", resource, ok)
			}
			title, subtitle := CardText(name, json.RawMessage(`{"command":"rm file"}`), "purpose", "local")
			if title != "删除文件" || !strings.Contains(subtitle, "本机") {
				t.Fatal(title, subtitle)
			}
		})
	}
	for _, command := range []string{"Remove-Item file", "DEL file", "Invoke-RestMethod https://example.invalid -Method POST"} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if !NeedsOwner("powershell", raw) {
			t.Fatal(command)
		}
	}
	for _, name := range []string{"cmd", "powershell"} {
		if ShellToolIsReadOnly(name, json.RawMessage(`{"command":"echo write"}`)) {
			t.Fatal("POSIX allowlist applied to", name)
		}
	}
}
