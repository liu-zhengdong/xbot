package config

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHoldKeysLeavesProcessEnviron(t *testing.T) {
	if os.Getenv("XBOT_HOLD_CHILD") == "1" {
		HoldKeys([]string{"XBOT_API_KEY", "DEEPSEEK_API_KEY"})
		if os.Getenv("XBOT_API_KEY") != "" || os.Getenv("DEEPSEEK_API_KEY") != "" {
			t.Fatal("provider keys still visible to os.Getenv")
		}
		if Getenv("XBOT_API_KEY") != "sekret-xbot" || Getenv("DEEPSEEK_API_KEY") != "sekret-deep" {
			t.Fatalf("memory %q %q", Getenv("XBOT_API_KEY"), Getenv("DEEPSEEK_API_KEY"))
		}
		if os.Getenv("OPENAI_API_KEY") != "sekret-openai" {
			t.Fatalf("unregistered key %q", os.Getenv("OPENAI_API_KEY"))
		}
		raw, err := os.ReadFile("/proc/self/environ")
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "sekret-xbot") || strings.Contains(text, "sekret-deep") {
			t.Fatalf("process environ still has provider keys:\n%s", strings.ReplaceAll(text, "\x00", "\n"))
		}
		if !strings.Contains(text, "sekret-openai") {
			t.Fatal("unregistered key was removed from the process environ")
		}
		return
	}

	env := withOverrides(os.Environ(), map[string]string{
		"XBOT_HOLD_CHILD":  "1",
		"XBOT_API_KEY":     "sekret-xbot",
		"DEEPSEEK_API_KEY": "sekret-deep",
		"OPENAI_API_KEY":   "sekret-openai",
	})
	cmd := exec.Command(os.Args[0], "-test.run=^TestHoldKeysLeavesProcessEnviron$", "-test.count=1")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func withOverrides(base []string, kv map[string]string) []string {
	skip := map[string]bool{}
	for name := range kv {
		skip[name] = true
	}
	out := make([]string, 0, len(base)+len(kv))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok && skip[name] {
			continue
		}
		out = append(out, entry)
	}
	for name, value := range kv {
		out = append(out, name+"="+value)
	}
	return out
}
