package bot

import "testing"

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
		if err := GrantGlobal(root, "desktop"); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := GlobalLinks(root)
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	for _, x := range []*Bot{a, b} {
		if !x.Grants("desktop") || len(x.EffectiveLinks()) != 2 || len(x.Config().Links) != 1 {
			t.Fatalf("%s %v %v", x.Name(), x.EffectiveLinks(), x.Config().Links)
		}
	}
	if err := RevokeGlobal(root, "desktop"); err != nil {
		t.Fatal(err)
	}
	if err := RevokeGlobal(root, "desktop"); err != nil {
		t.Fatal(err)
	}
	if a.Grants("desktop") || b.Grants("desktop") {
		t.Fatal("still granted")
	}
}
