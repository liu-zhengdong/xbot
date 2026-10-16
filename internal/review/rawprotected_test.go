package review

import (
	"path/filepath"
	"testing"
)

// TestRawProtectedPathSegments pins the raw fallback to whole path segments.
// Source files whose path merely contains "vault" as a substring (for example
// this repository's own internal/vault/proxy.go) must not be treated as a
// protected path: only a real vault directory or the shared keys/vault store
// counts. The repository source case is what made every edit under
// internal/vault/ ask for approval on a machine with a vault configured.
func TestRawProtectedPathSegments(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "repo source under internal/vault", input: "internal/vault/proxy.go", want: false},
		{name: "repo source shell word", input: "sed -n 1,20p internal/vault/proxy.go", want: false},
		{name: "vault prefix in a longer name", input: "internal/vaultmigrate/x.go", want: false},
		{name: "vault as a middle segment only", input: "src/vault/proxy.go", want: false},
		{name: "name that only contains vault", input: "myvault.key", want: false},
		{name: "go test of the vault package", input: "go test ./internal/vault/...", want: false},
		{name: "docs mentioning vault", input: "docs/vault.md", want: false},

		{name: "bot vault directory", input: "bots/ada/vault", want: true},
		{name: "file inside a bot vault", input: "bots/ada/vault/secrets.enc", want: true},
		{name: "relative vault dir", input: "vault", want: true},
		{name: "cat inside a vault", input: "cat bots/x/vault/key", want: true},
		{name: "shared keys vault", input: "keys/vault", want: true},
		{name: "file under shared keys vault", input: "keys/vault/ada.key", want: true},
		{name: "absolute vault file", input: "/data/bots/ada/vault/key", want: true},
		{name: "secrets file anywhere", input: "bots/ada/secrets.enc", want: true},
	}
	for _, c := range cases {
		if got := rawProtected(c.input); got != c.want {
			t.Errorf("%s: rawProtected(%q) = %v, want %v", c.name, c.input, got, c.want)
		}
	}
}

// TestTouchesProtectedRepoSourceNotBlocked is the end-to-end form of the same
// rule: with a real vault configured, reading or editing this repository's own
// vault package must not look like touching protected data, while the actual
// vault directory still does.
func TestTouchesProtectedRepoSourceNotBlocked(t *testing.T) {
	root, botDir, _, keysDir, protected := protectedFixture(t)

	repoSource := "internal/vault/proxy.go"
	if TouchesProtected(repoSource, root, protected) {
		t.Errorf("repo source %q was treated as protected", repoSource)
	}
	if TouchesProtected("sed -n 1,20p "+repoSource, root, protected) {
		t.Errorf("shell read of repo source %q was treated as protected", repoSource)
	}
	if TouchesProtected("internal/vault/proxy_escaped_test.go", root, protected) {
		t.Error("repo test file was treated as protected")
	}

	if !TouchesProtected(filepath.Join(botDir, "vault"), "", protected) {
		t.Error("the bot vault directory is no longer protected")
	}
	if !TouchesProtected(filepath.Join(keysDir, "ada.key"), "", protected) {
		t.Error("a shared key file is no longer protected")
	}
}
