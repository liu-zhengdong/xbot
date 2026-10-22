package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xbot/internal/web"

	"xbot/internal/bot"
	"xbot/internal/config"
)

func TestServeRejectsMissingBotModelKeysBeforeListening(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "binding"}[named], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XBOT_API_KEY", "")
			t.Setenv("XBOT_M2_MISSING_KEY", "")
			b, err := bot.Create(root, "ada", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "XBOT_API_KEY"
			if named {
				if err := config.Add(root, config.Provider{Name: "test", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_M2_MISSING_KEY"}, config.ModelSpec{Name: "fake", Context: 10000}); err != nil {
					t.Fatal(err)
				}
				if err := b.SetBinding("test", "fake"); err != nil {
					t.Fatal(err)
				}
				// A working default must not hide the second bot's missing bound key.
				t.Setenv("XBOT_API_KEY", "fake-default")
				want = "XBOT_M2_MISSING_KEY"
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			listened := false
			timers := false
			err = runServe(ctx, root, serveConfig{HTTPAddr: "127.0.0.1:0", Interval: time.Hour, OnListening: func(_ *web.Server, _ net.Addr) { listened = true }, DeliverTimers: func(string, time.Time) error { timers = true; return nil }})
			if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "ada") || listened || timers {
				t.Fatalf("preflight: err=%v listened=%v timers=%v", err, listened, timers)
			}
		})
	}
}

func TestDevEnvFileIsLiteralAndKeepsSetVariables(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/xbot-dev")
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := strings.Cut(string(raw), "load_env() {")
	if !ok {
		t.Fatal("missing dev env loader")
	}
	body, _, ok = strings.Cut(body, "\n}\n")
	if !ok {
		t.Fatal("missing loader end")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-exist")
	data := "XBOT_M2_SET=replaced\nXBOT_M2_EMPTY=replaced\nXBOT_M2_LITERAL='$(touch " + marker + ")'\nXBOT_M2_BACKTICK=`touch " + marker + "`\nXBOT_M2_EQ=a=b\n# ignored\ninvalid-name=bad\n"
	if err := os.WriteFile(filepath.Join(root, "env"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	script := "set -eu\nload_env() {" + body + "\n}\nDATA=$1\nexport XBOT_M2_SET=kept XBOT_M2_EMPTY=''\nload_env\n[ \"$XBOT_M2_SET\" = kept ] && [ -z \"$XBOT_M2_EMPTY\" ] && [ \"$XBOT_M2_EQ\" = 'a=b' ]\n[[ \"$XBOT_M2_LITERAL\" = '$('* ]] && [[ \"$XBOT_M2_BACKTICK\" = '`'* ]]\n"
	cmd := exec.Command("bash", "-c", script, "env-test", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("env loader: %v: %s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("env value executed")
	}
}
