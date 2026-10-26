//go:build windows

package tools

import (
	"context"
	"strings"
	"testing"
)

func TestCmdPreservesInnerQuotes(t *testing.T) {
	script := `echo "two words" & echo done`
	cmd := (interpreter{name: "cmd", path: `C:\Windows\System32\cmd.exe`}).command(context.Background(), script)
	if cmd.SysProcAttr == nil || !strings.HasSuffix(cmd.SysProcAttr.CmdLine, ` /d /s /c "`+script+`"`) {
		t.Fatalf("cmd quotes: %+v", cmd.SysProcAttr)
	}
}
