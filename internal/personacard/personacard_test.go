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
	c, err := Create(root, "u1", "  写作\n助手 ", "说话短一点。\r\n先给结论。")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "写作 助手" || c.Body != "说话短一点。\n先给结论。" || !ValidID(c.ID) {
		t.Fatalf("%+v", c)
	}
	got, err := Get(root, "u1", c.ID)
	if err != nil || got.Name != c.Name || got.Body != c.Body {
		t.Fatalf("%+v %v", got, err)
	}
	name, body := "编辑", "只改正文。"
	if _, err := Update(root, "u1", c.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(root, "u1", c.ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	got, _ = Get(root, "u1", c.ID)
	if got.Name != "编辑" || got.Body != "只改正文。" {
		t.Fatalf("%+v", got)
	}
	list, err := List(root, "u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if other, _ := List(root, "u2"); len(other) != 0 {
		t.Fatalf("another owner sees %+v", other)
	}
	if err := Delete(root, "u1", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(root, "u1", c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := Delete(root, "u1", c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "u1", "x", "  \n "); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if _, err := Create(root, "u1", "x", strings.Repeat("a", MaxBody+1)); !errors.Is(err, ErrLarge) {
		t.Fatal(err)
	}
	if _, err := Create(root, "../u1", "x", "body"); err == nil {
		t.Fatal("bad owner accepted")
	}
	// Ids are only ever plain hex directory names: nothing can reach outside the library.
	outside := filepath.Join(root, "persona-cards", "u1", "..", "secret")
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "card.md"), []byte("name: x\n\nsecret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", "..", "", "ABCDEF0123456789", "a/b"} {
		if _, err := Get(root, "u1", id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("id %q: %v", id, err)
		}
		if err := Delete(root, "u1", id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete %q: %v", id, err)
		}
	}
	long := strings.Repeat("名", MaxName+10)
	c, err := Create(root, "u1", long, "body")
	if err != nil || len([]rune(c.Name)) != MaxName {
		t.Fatalf("%q %v", c.Name, err)
	}
}

func TestImportMakesANewCardAndSuffixesSameName(t *testing.T) {
	root := t.TempDir()
	first, err := Import(root, "u1", "writer.md", "# 写作助手\n\n说话短一点。", false)
	if err != nil || first.Name != "写作助手" || !strings.Contains(first.Body, "说话短一点") {
		t.Fatalf("%+v %v", first, err)
	}
	dup, err := Import(root, "u1", "other.md", "# 写作助手\n\n另一份。", false)
	if !errors.Is(err, ErrExists) || dup.Name != "写作助手" {
		t.Fatalf("%+v %v", dup, err)
	}
	if list, _ := List(root, "u1"); len(list) != 1 {
		t.Fatalf("unconfirmed import wrote: %+v", list)
	}
	second, err := Import(root, "u1", "other.md", "# 写作助手\n\n另一份。", true)
	if err != nil || second.Name != "写作助手 (2)" || second.ID == first.ID {
		t.Fatalf("%+v %v", second, err)
	}
	third, err := Import(root, "u1", "other.md", "# 写作助手\n\n第三份。", true)
	if err != nil || third.Name != "写作助手 (3)" {
		t.Fatalf("%+v %v", third, err)
	}
	// The original is untouched: import never overwrites.
	got, _ := Get(root, "u1", first.ID)
	if got.Body != first.Body {
		t.Fatalf("overwrote %+v", got)
	}
	// A first line too long for a name falls back to the file name.
	c, err := Import(root, "u1", "../../etc/notes.md", strings.Repeat("长", MaxName+1)+"\n正文", false)
	if err != nil || c.Name != "notes" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := Import(root, "u1", "e.md", "\n\n", false); !errors.Is(err, ErrEmpty) {
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
			if _, err := Import(root, "u1", "a.md", "# 同名\n\nbody", true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	list, err := List(root, "u1")
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
		if _, err := Create(root, "u1", "x", "body"); err != nil {
			t.Fatal(err)
		}
		list, _ = List(root, "u1")
	}
	if _, err := Create(root, "u1", "x", "body"); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	if _, err := Import(root, "u1", "x.md", "x", false); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
}
