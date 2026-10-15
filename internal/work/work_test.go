package work_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/tools"
	"xbot/internal/work"
)

func TestShellUsesBotDir(t *testing.T) {
	root := t.TempDir()
	dir, shared, err := work.Open(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	sh := tools.NewShell()
	sh.Dir = dir
	sh.Shared = shared
	args, _ := json.Marshal(map[string]string{"command": "pwd; printf ' %s' \"$XBOT_SHARED\""})
	out, err := sh.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, shared) {
		t.Fatalf("%s", out)
	}
}
