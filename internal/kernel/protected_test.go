package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// protectedKernel builds a kernel in allow mode whose bot has a vault already
// on disk, so a direct vault read must still ask.
func protectedKernel(t *testing.T) (*Kernel, string) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools)
	workDir := filepath.Join(b.Root, "work", b.Name())
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.VaultDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.VaultDir(), "secrets.enc"), []byte("ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	return &Kernel{Bot: b, WorkDir: workDir}, workDir
}

func TestReviewCallProtectedReadFileAsksInAllowMode(t *testing.T) {
	k, workDir := protectedKernel(t)
	if mode, err := machine.Get(k.Bot.Root, machine.Local); err != nil || mode != machine.Allow {
		t.Fatalf("machine mode %q err=%v, want allow", mode, err)
	}
	read := &tools.ReadFile{Root: workDir}

	need, summary, err := reviewedTool{Tool: read, k: k}.Confirm(
		json.RawMessage(`{"path":"` + filepath.Join(k.Bot.VaultDir(), "secrets.enc") + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !need {
		t.Fatal("read_file of the bot's own vault was allowed")
	}
	if !strings.Contains(summary, review.ProtectedSuffix) {
		t.Fatalf("summary %q does not call out the vault", summary)
	}

	// A relative traversal from the work dir reaches the vault too.
	need, _, err = reviewedTool{Tool: read, k: k}.Confirm(
		json.RawMessage(`{"path":"../../bots/ada/vault/secrets.enc"}`))
	if err != nil || !need {
		t.Fatalf("relative traversal need=%v err=%v", need, err)
	}

	// The shared key directory is protected as well.
	need, _, err = reviewedTool{Tool: read, k: k}.Confirm(
		json.RawMessage(`{"path":"` + filepath.Join(k.Bot.VaultKeyPath()) + `"}`))
	if err != nil || !need {
		t.Fatalf("key read need=%v err=%v", need, err)
	}

	// An ordinary file still needs no card.
	ordinary := filepath.Join(workDir, "notes.txt")
	if err := os.WriteFile(ordinary, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	need, _, err = reviewedTool{Tool: read, k: k}.Confirm(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || need {
		t.Fatalf("ordinary read need=%v err=%v", need, err)
	}
}

func TestReviewCallProtectedWriteAndEditAskInAllowMode(t *testing.T) {
	k, workDir := protectedKernel(t)
	cases := []struct {
		name string
		tool tools.Tool
		args string
		want bool
	}{
		{
			name: "write into the vault",
			tool: &tools.WriteFile{Root: workDir},
			args: `{"path":"` + filepath.Join(k.Bot.VaultDir(), "secrets.enc") + `","content":"x"}`,
			want: true,
		},
		{
			name: "edit the vault key",
			tool: &tools.EditFile{Root: workDir},
			args: `{"path":"` + k.Bot.VaultKeyPath() + `","old_string":"a","new_string":"b"}`,
			want: true,
		},
		{
			name: "write an ordinary file",
			tool: &tools.WriteFile{Root: workDir},
			args: `{"path":"notes.txt","content":"x"}`,
			want: false,
		},
	}
	for _, c := range cases {
		need, summary, err := reviewedTool{Tool: c.tool, k: k}.Confirm(json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if need != c.want {
			t.Errorf("%s: need=%v want=%v", c.name, need, c.want)
		}
		if c.want && !strings.Contains(summary, review.ProtectedSuffix) {
			t.Errorf("%s: summary %q missing the vault suffix", c.name, summary)
		}
	}
}

func TestReviewCallProtectedShellAsksInAllowMode(t *testing.T) {
	k, _ := protectedKernel(t)
	shell := tools.NewShell()
	botDir := k.Bot.VaultDir()[:len(k.Bot.VaultDir())-len("/vault")]

	cases := []struct {
		name string
		args string
		want bool
	}{
		{
			name: "glob in bot dir",
			args: `{"command":"cat vault/*","cwd":"` + botDir + `"}`,
			want: true,
		},
		{
			name: "absolute secrets file",
			args: `{"command":"cat ` + filepath.Join(k.Bot.VaultDir(), "secrets.enc") + `"}`,
			want: true,
		},
		{
			name: "relative through work dir cwd",
			args: `{"command":"cat ../../bots/ada/vault/*"}`,
			want: true,
		},
		{
			name: "key dir",
			args: `{"command":"ls ` + filepath.Dir(k.Bot.VaultKeyPath()) + `"}`,
			want: true,
		},
		{
			name: "ordinary command",
			args: `{"command":"ls -la"}`,
			want: false,
		},
		{
			name: "ordinary file",
			args: `{"command":"cat notes.txt"}`,
			want: false,
		},
	}
	for _, c := range cases {
		need, summary, err := reviewedTool{Tool: shell, k: k}.Confirm(json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if need != c.want {
			t.Errorf("%s: need=%v want=%v", c.name, need, c.want)
		}
		if c.want && !strings.Contains(summary, review.ProtectedSuffix) {
			t.Errorf("%s: summary %q missing the vault suffix", c.name, summary)
		}
	}
}

func TestReviewCallLinkSecretsFileAsks(t *testing.T) {
	k, workDir := protectedKernel(t)
	if err := os.WriteFile(k.Bot.SecretPath(), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	read := &tools.ReadFile{Root: workDir}
	need, _, err := reviewedTool{Tool: read, k: k}.Confirm(json.RawMessage(`{"path":"` + k.Bot.SecretPath() + `"}`))
	if err != nil || !need {
		t.Fatalf("read_file secrets.json need=%v err=%v", need, err)
	}
	sh := &tools.Shell{}
	need, _, err = reviewedTool{Tool: sh, k: k}.Confirm(json.RawMessage(`{"command":"cat ../../bots/ada/secrets.json"}`))
	if err != nil || !need {
		t.Fatalf("shell cat secrets.json need=%v err=%v", need, err)
	}
}

func TestReviewCallWebAndProvidersFilesAsk(t *testing.T) {
	k, workDir := protectedKernel(t)
	read := &tools.ReadFile{Root: workDir}
	for _, name := range []string{"web.json", "providers.json"} {
		path := filepath.Join(k.Bot.Root, name)
		if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		need, summary, err := reviewedTool{Tool: read, k: k}.Confirm(json.RawMessage(`{"path":"` + path + `"}`))
		if err != nil || !need {
			t.Fatalf("%s need=%v err=%v", name, need, err)
		}
		if !strings.Contains(summary, review.ProtectedSuffix) {
			t.Fatalf("%s summary %q", name, summary)
		}
	}
}
