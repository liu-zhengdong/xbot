package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileKeepsSetVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	body := "\ufeff# keys\nexport XBOT_T_A=\"one\" # prod\nXBOT_T_B='two'\nXBOT_T_C=three\nXBOT_T_D=four # note\nnot a line\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_T_C", "kept")
	for _, k := range []string{"XBOT_T_A", "XBOT_T_B", "XBOT_T_D"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("XBOT_T_A") != "one" || os.Getenv("XBOT_T_B") != "two" || os.Getenv("XBOT_T_C") != "kept" || os.Getenv("XBOT_T_D") != "four" {
		t.Fatalf("env: %q %q %q", os.Getenv("XBOT_T_A"), os.Getenv("XBOT_T_B"), os.Getenv("XBOT_T_C"))
	}
	if err := loadEnvFile(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	if got := launchArgs([]string{"-psn_0_12345", "--root", "x"}); len(got) != 2 || got[0] != "--root" {
		t.Fatalf("launch args: %v", got)
	}
}

func TestDefaultCommandRunsWithoutArgs(t *testing.T) {
	old := defaultCommand
	defaultCommand = "version"
	defer func() { defaultCommand = old }()
	if err := run([]string{"--root", t.TempDir()}); err != nil {
		t.Fatalf("no-arg launch: %v", err)
	}
}
