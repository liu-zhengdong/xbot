package personacard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCreateUpdateDelete(t *testing.T) {
	root := t.TempDir()
	c, err := Create(root, "  写作\n助手 ", "说话短一点。\r\n先给结论。")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "写作 助手" || c.Body != "说话短一点。\n先给结论。" || !ValidID(c.ID) {
		t.Fatalf("%+v", c)
	}
	got, err := Get(root, c.ID)
	if err != nil || got.Name != c.Name || got.Body != c.Body {
		t.Fatalf("%+v %v", got, err)
	}
	name, body := "编辑", "只改正文。"
	if _, err := Update(root, c.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(root, c.ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	got, _ = Get(root, c.ID)
	if got.Name != "编辑" || got.Body != "只改正文。" {
		t.Fatalf("%+v", got)
	}
	list, err := List(root)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if err := Delete(root, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(root, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := Delete(root, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "x", "  \n "); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if _, err := Create(root, "x", strings.Repeat("a", MaxBody+1)); !errors.Is(err, ErrLarge) {
		t.Fatal(err)
	}
	// Ids are only ever plain hex directory names: nothing can reach outside the library.
	outside := filepath.Join(root, "secret")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "card.md"), []byte("name: x\n\nsecret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", "..", "", "ABCDEF0123456789", "a/b"} {
		if _, err := Get(root, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("id %q: %v", id, err)
		}
		if err := Delete(root, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete %q: %v", id, err)
		}
	}
	long := strings.Repeat("名", MaxName+10)
	c, err := Create(root, long, "body")
	if err != nil || len([]rune(c.Name)) != MaxName {
		t.Fatalf("%q %v", c.Name, err)
	}
}

func TestImportMakesANewCardAndSuffixesSameName(t *testing.T) {
	root := t.TempDir()
	first, err := Import(root, "writer.md", "# 写作助手\n\n说话短一点。", false)
	if err != nil || first.Name != "写作助手" || !strings.Contains(first.Body, "说话短一点") {
		t.Fatalf("%+v %v", first, err)
	}
	dup, err := Import(root, "other.md", "# 写作助手\n\n另一份。", false)
	if !errors.Is(err, ErrExists) || dup.Name != "写作助手" {
		t.Fatalf("%+v %v", dup, err)
	}
	if list, _ := List(root); len(list) != 1 {
		t.Fatalf("unconfirmed import wrote: %+v", list)
	}
	second, err := Import(root, "other.md", "# 写作助手\n\n另一份。", true)
	if err != nil || second.Name != "写作助手 (2)" || second.ID == first.ID {
		t.Fatalf("%+v %v", second, err)
	}
	third, err := Import(root, "other.md", "# 写作助手\n\n第三份。", true)
	if err != nil || third.Name != "写作助手 (3)" {
		t.Fatalf("%+v %v", third, err)
	}
	// The original is untouched: import never overwrites.
	got, _ := Get(root, first.ID)
	if got.Body != first.Body {
		t.Fatalf("overwrote %+v", got)
	}
	// A first line too long for a name falls back to the file name.
	c, err := Import(root, "../../etc/notes.md", strings.Repeat("长", MaxName+1)+"\n正文", false)
	if err != nil || c.Name != "notes" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := Import(root, "e.md", "\n\n", false); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
}

func TestLibraryCapAndConcurrentCreates(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Import(root, "a.md", "# 同名\n\nbody", true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	list, err := List(root)
	if err != nil || len(list) != 16 {
		t.Fatalf("%d %v", len(list), err)
	}
	seen := map[string]bool{}
	for _, c := range list {
		if seen[c.Name] {
			t.Fatalf("two cards named %q", c.Name)
		}
		seen[c.Name] = true
	}
	for len(list) < MaxCards {
		if _, err := Create(root, "x", "body"); err != nil {
			t.Fatal(err)
		}
		list, _ = List(root)
	}
	if _, err := Create(root, "x", "body"); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	if _, err := Import(root, "x.md", "x", false); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
}

// writeCard puts a card where the old persona-cards/<owner>/<id>/ layout kept it.
func writeCard(t *testing.T, dir, id, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, "card.md"), []byte("name: "+name+"\n\n"+body+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// Cards from the per-owner layout move up and keep their ids, so bindings still
// resolve; the owner's default and seed markers come with them.
func TestLiftsOwnerLibrary(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(Dir(root), "u501")
	writeCard(t, old, "0123456789abcdef", "我的卡", "只说一句。")
	writeCard(t, old, "fedcba9876543210", DefaultName, PartnerPersona)
	if err := os.WriteFile(filepath.Join(old, defaultFile), []byte("fedcba9876543210\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := markSeeded(old); err != nil {
		t.Fatal(err)
	}
	if c, err := Get(root, "0123456789abcdef"); err != nil || c.Body != "只说一句。" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("owner directory left: %v", err)
	}
	if !has(filepath.Join(Dir(root), "0123456789abcdef"), "card.md") || !has(Dir(root), seededFile) {
		t.Fatal("cards or marker not flat")
	}
	if def, ok := Default(root); !ok || def.ID != "fedcba9876543210" {
		t.Fatalf("%+v %v", def, ok)
	}
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	if cards, _ := List(root); len(cards) != 2 {
		t.Fatalf("marked library reseeded: %+v", cards)
	}
	c, err := Create(root, "新卡", "新正文。")
	if err != nil || !has(filepath.Join(Dir(root), c.ID), "card.md") {
		t.Fatalf("%+v %v", c, err)
	}
}

// An id already in the flat library is never overwritten: an identical copy is
// dropped, a different card moves under a new id.
func TestLiftKeepsBothOnClash(t *testing.T) {
	root := t.TempDir()
	writeCard(t, Dir(root), "0123456789abcdef", "现有", "现有正文。")
	writeCard(t, Dir(root), "1111111111111111", "同卡", "同正文。")
	old := filepath.Join(Dir(root), "u501")
	writeCard(t, old, "0123456789abcdef", "旧卡", "旧正文。")
	writeCard(t, old, "1111111111111111", "同卡", "同正文。")
	cards, err := List(root)
	if err != nil || len(cards) != 3 {
		t.Fatalf("%+v %v", cards, err)
	}
	if c, _ := Get(root, "0123456789abcdef"); c.Name != "现有" {
		t.Fatalf("overwritten: %+v", c)
	}
	names := map[string]bool{}
	for _, c := range cards {
		names[c.Name] = true
	}
	if !names["旧卡"] {
		t.Fatalf("lost the clashing card: %+v", cards)
	}
}
