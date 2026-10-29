package bot

import (
	"os"
	"testing"
)

func TestGlobalLinksReachEveryBot(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "ada", "p", []string{"terminal"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(root, "bob", "p", []string{"terminal"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := GrantGlobal(root, "browser"); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := GlobalLinks(root)
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	for _, x := range []*Bot{a, b} {
		if !x.Grants("browser") || len(x.EffectiveLinks()) != 1 || len(x.Config().Links) != 1 {
			t.Fatalf("%s %v %v", x.Name(), x.EffectiveLinks(), x.Config().Links)
		}
	}
	if err := RevokeGlobal(root, "browser"); err != nil {
		t.Fatal(err)
	}
	if err := RevokeGlobal(root, "browser"); err != nil {
		t.Fatal(err)
	}
	if a.Grants("browser") || b.Grants("browser") {
		t.Fatal("still granted")
	}
}

func TestLegacyOrganGrantsAreIgnored(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "p", []string{"memory", "skills", "browser"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AppsPath(root), []byte(`{"links":["desktop","watch"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := b.EffectiveLinks(); len(got) != 1 || got[0] != "browser" {
		t.Fatalf("effective links: %v", got)
	}
	if err := b.GrantLink("timer"); err != nil {
		t.Fatal(err)
	}
	if err := GrantGlobal(root, "webhook"); err != nil {
		t.Fatal(err)
	}
	if b.Grants("timer") || b.Grants("memory") || b.Grants("desktop") {
		t.Fatal("legacy native grants became links")
	}
	if len(b.Config().Links) != 3 {
		t.Fatalf("new native grant was persisted: %v", b.Config().Links)
	}
	got, err := GlobalLinks(root)
	if err != nil || len(got) != 2 {
		t.Fatalf("new global native grant was persisted: %v %v", got, err)
	}
}
