package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledSectionOneLineEach(t *testing.T) {
	one := InstalledSection([]InstalledRef{{ID: "webhook", Summary: "接收 HTTP 回调，写成不可信事件。"}})
	two := InstalledSection([]InstalledRef{
		{ID: "webhook", Summary: "接收 HTTP 回调，写成不可信事件。"},
		{ID: "watch", Summary: "路径变化写成不可信事件，正文只有路径和元数据。"},
	})
	if strings.Count(two, "\n") != strings.Count(one, "\n")+1 {
		t.Fatalf("lines %q %q", one, two)
	}
	if !strings.Contains(two, "webhook:") || !strings.Contains(two, "watch:") {
		t.Fatal(two)
	}
	if strings.Contains(two, "inputSchema") || strings.Contains(two, "\"type\"") || strings.Contains(two, "parameters") {
		t.Fatal(two)
	}
}

func TestInstalledSectionCapsToIDs(t *testing.T) {
	const marker = "SUMMARY-SHOULD-DROP"
	refs := make([]InstalledRef, 0, 40)
	for i := 0; i < 40; i++ {
		refs = append(refs, InstalledRef{ID: "link", Summary: marker + " " + strings.Repeat("字", 30)})
	}
	text := InstalledSection(refs)
	if strings.Contains(text, marker) {
		t.Fatalf("summaries kept over cap: %s", text)
	}
	if strings.Count(text, "\n") < 39 || !strings.Contains(text, "link\n") {
		t.Fatalf("ids missing: %s", text)
	}
}

func TestWriteSecretsOmitsEmptyAndSetsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := WriteSecrets(path, map[string]string{"TELEGRAM_BOT_TOKEN": "super-secret-value", "": "x", "BLANK": ""}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	got, err := ReadSecrets(path)
	if err != nil || got["TELEGRAM_BOT_TOKEN"] != "super-secret-value" || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if err := DropSecrets(path, []string{"TELEGRAM_BOT_TOKEN"}); err != nil {
		t.Fatal(err)
	}
	got, err = ReadSecrets(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}
