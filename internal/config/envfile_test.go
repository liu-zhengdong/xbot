package config

import (
	"os"
	"strings"
	"testing"
)

func TestSaveEnvKeyKeepsOtherLines(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(EnvFile(root), []byte("\ufeffA=1\r\nXBOT_API_KEY=old\r\n# note\r\nXBOT_API_KEY=dup\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveEnvKey(root, "XBOT_API_KEY", "sk-new-12345678"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(EnvFile(root))
	if string(raw) != "\ufeffA=1\nXBOT_API_KEY=sk-new-12345678\n# note\n" {
		t.Fatalf("%q", raw)
	}
	for _, bad := range []string{"", "a b", `"q"`, "x#y"} {
		if err := SaveEnvKey(root, "XBOT_API_KEY", bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	fresh := t.TempDir()
	if err := SaveEnvKey(fresh, "XBOT_API_KEY", "sk-new-12345678"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(EnvFile(fresh))
	if !strings.HasSuffix(string(raw), "XBOT_API_KEY=sk-new-12345678\n") || strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("%q", raw)
	}
}
