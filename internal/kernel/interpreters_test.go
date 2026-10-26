package kernel

import (
	"context"
	"encoding/json"
	"testing"
	"xbot/internal/agenttype"
	"xbot/internal/tools"
)

func TestInterpreterScopesAndReadOnlyPolicy(t *testing.T) {
	k := &Kernel{WorkDir: t.TempDir()}
	list := k.toolsForScope("interpreter-test")
	got := map[string]tools.Tool{}
	for _, tool := range list {
		got[tool.Name()] = tool
	}
	if got["shell"] != nil {
		t.Fatal("legacy alias in scope")
	}
	for _, sh := range tools.NewShells() {
		tool := got[sh.Name()]
		if tool == nil {
			t.Fatal("missing scoped", sh.Name())
		}
		ro := readOnlyShell{tool}
		if _, err := ro.Run(context.Background(), json.RawMessage(`{"command":"echo x > forbidden","description":"Try writing"}`)); err == nil {
			t.Fatal("read-only write accepted", sh.Name())
		}
		ty := agenttype.Type{Tools: []string{sh.Name()}}
		if writeResource(ty, k.WorkDir) == "" {
			t.Fatal("interpreter omitted from write resource", sh.Name())
		}
	}
}
