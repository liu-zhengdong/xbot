package link

import (
	"strings"
	"testing"

	"xbot/internal/tools"
)

func TestTerminalAccount(t *testing.T) {
	id, display, err := Account()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "uid:") || display == "" {
		t.Fatalf("id=%q display=%q", id, display)
	}
}

func TestBaseToolNames(t *testing.T) {
	names := map[string]bool{}
	for _, sh := range tools.NewShells() {
		names[sh.Name()] = true
	}
	for _, f := range []tools.Tool{&tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}} {
		names[f.Name()] = true
	}
	for _, want := range []string{"sh", "read_file", "write_file", "edit_file"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
}
