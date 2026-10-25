package main

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/config"
)

func TestStartupHoldsSavedSearchKeysBeforeAnySearch(t *testing.T) {
	root := t.TempDir()
	defer config.ForgetKeys()
	const key = "fake-saved-search-after-restart"
	if err := config.SaveSearchCredential(root, "deepseek", key); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWebSearch(root, false, "auto", "ddg"); err != nil {
		t.Fatal(err)
	}
	config.ForgetKeys()
	if err := run([]string{"--root", root, "version"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(config.EnvFile(root))
	if err != nil {
		t.Fatal(err)
	}
	redacted := config.RedactHeld(string(raw))
	if strings.Contains(redacted, key) || !strings.Contains(redacted, "[secret]") {
		t.Fatal("saved, unselected search credential escaped startup redaction")
	}
	if _, ok := os.LookupEnv(config.SearchSecretName("deepseek")); ok {
		t.Fatal("search key exported to children")
	}
}
