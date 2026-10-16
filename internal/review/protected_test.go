package review

import (
	"os"
	"path/filepath"
	"testing"
)

// protectedFixture builds a data root with one bot vault and the shared keys
// directory, then returns the protected list the kernel would pass.
func protectedFixture(t *testing.T) (root, botDir, vaultDir, keysDir string, protected []string) {
	t.Helper()
	root = t.TempDir()
	botDir = filepath.Join(root, "bots", "ada")
	vaultDir = filepath.Join(botDir, "vault")
	keysDir = filepath.Join(root, "keys", "vault")
	for _, dir := range []string{vaultDir, keysDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "secrets.enc"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, botDir, vaultDir, keysDir, []string{vaultDir, keysDir}
}

func TestTouchesProtected(t *testing.T) {
	root, botDir, vaultDir, keysDir, protected := protectedFixture(t)
	other := filepath.Join(root, "shared", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(other), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		input     string
		cwd       string
		protected []string
		want      bool
	}{
		{name: "absolute vault file", input: filepath.Join(vaultDir, "secrets.enc"), protected: protected, want: true},
		{name: "absolute vault dir", input: vaultDir, protected: protected, want: true},
		{name: "absolute key file", input: filepath.Join(keysDir, "ada.key"), protected: protected, want: true},
		{name: "relative via cwd", input: "vault/key", cwd: botDir, protected: protected, want: true},
		{name: "relative parent traversal", input: "../ada/vault/secrets.enc", cwd: filepath.Join(root, "bots", "bob"), protected: protected, want: true},
		{name: "shell cat vault file", input: "cat vault/secrets.enc", cwd: botDir, protected: protected, want: true},
		{name: "shell cat vault glob", input: "cat vault/*", cwd: botDir, protected: protected, want: true},
		{name: "shell chained", input: "echo hi && cat vault/key", cwd: botDir, protected: protected, want: true},
		{name: "shell keys dir", input: "ls keys/vault", cwd: root, protected: protected, want: true},
		{name: "raw fallback keys/vault", input: "keys/vault", protected: protected, want: true},
		{name: "unrelated relative", input: "notes.txt", cwd: botDir, protected: protected, want: false},
		{name: "unrelated absolute", input: other, protected: protected, want: false},
		{name: "unrelated shell", input: "ls -la /home", cwd: botDir, protected: protected, want: false},
		{name: "no protected paths", input: filepath.Join(vaultDir, "key"), protected: nil, want: false},
	}
	for _, c := range cases {
		if got := TouchesProtected(c.input, c.cwd, c.protected); got != c.want {
			t.Errorf("%s: TouchesProtected(%q, %q) = %v, want %v", c.name, c.input, c.cwd, got, c.want)
		}
	}
}

func TestTouchesProtectedSymlink(t *testing.T) {
	root, _, vaultDir, _, protected := protectedFixture(t)
	link := filepath.Join(root, "shortcut")
	if err := os.Symlink(vaultDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !TouchesProtected(filepath.Join(link, "secrets.enc"), "", protected) {
		t.Fatal("symlink into the vault was not caught")
	}
	// A file-tool path that does not exist yet still resolves through the
	// symlinked parent.
	if !TouchesProtected(filepath.Join(link, "newfile"), "", protected) {
		t.Fatal("not-yet-existing path under the symlinked vault was not caught")
	}
}

func TestTouchesProtectedNoFalsePositiveOnLongerName(t *testing.T) {
	_, _, vaultDir, _, protected := protectedFixture(t)
	sibling := vaultDir + "-backup"
	if TouchesProtected(sibling, "", protected) {
		t.Fatalf("%s matched the vault prefix", sibling)
	}
	if TouchesProtected(filepath.Join(sibling, "x"), "", protected) {
		t.Fatal("sibling of the vault matched as if under it")
	}
}

// TestTouchesProtectedResolvesPaths uses a protected directory whose name does
// not contain "vault", so the raw string fallback cannot fire and the path
// resolution is what is under test: relative words against cwd, parent
// traversal, globs, and symlinks.
func TestTouchesProtectedResolvesPaths(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "area", "secret")
	if err := os.MkdirAll(filepath.Join(secret, "inner"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "token"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	protected := []string{secret}
	cwd := root
	inner := filepath.Join(root, "area", "inner")
	if err := os.MkdirAll(inner, 0700); err != nil {
		t.Fatal(err)
	}

	if !TouchesProtected("area/secret/token", cwd, protected) {
		t.Error("relative path against cwd not caught")
	}
	if !TouchesProtected("../secret/token", inner, protected) {
		t.Error("parent traversal not caught")
	}
	if !TouchesProtected("area/secret/*", cwd, protected) {
		t.Error("glob prefix not caught")
	}
	if !TouchesProtected("area/secret/inner/deep/*", cwd, protected) {
		t.Error("deeper glob not caught")
	}
	if TouchesProtected("area/other/token", cwd, protected) {
		t.Error("unrelated relative path matched")
	}

	link := filepath.Join(root, "alias")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !TouchesProtected(filepath.Join(link, "token"), "", protected) {
		t.Error("symlink to the protected dir not caught")
	}
	if !TouchesProtected(filepath.Join(link, "does-not-exist"), "", protected) {
		t.Error("not-yet-existing path under a symlinked protected dir not caught")
	}
}
