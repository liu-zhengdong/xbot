package review

import (
	"encoding/json"
	"testing"
)

func TestResourceOf(t *testing.T) {
	cases := []struct {
		name, args, want string
		ok               bool
	}{
		{"write_file", `{"path":"a.txt"}`, "file:/w/a.txt", true},
		{"edit_file", `{"path":"/x/../y/b.go"}`, "file:/y/b.go", true},
		{"read_file", `{"path":"a.txt"}`, "", false},
		{"shell", `{"command":"ls -la"}`, "", false},
		{"shell", `{"command":"git status"}`, "", false},
		{"shell", `{"command":"ls > out"}`, "dir:/w", true},
		{"shell", `{"command":"go build ./...","cwd":"/r"}`, "dir:/r", true},
		{"code_start", `{"repo":"o/r"}`, "repo:o/r", true},
	}
	for _, c := range cases {
		got, ok := ResourceOf(c.name, json.RawMessage(c.args), "/w")
		if got != c.want || ok != c.ok {
			t.Errorf("%s %s = %q %v, want %q %v", c.name, c.args, got, ok, c.want, c.ok)
		}
	}
}
