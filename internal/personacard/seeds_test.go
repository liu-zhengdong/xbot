package personacard

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEnsureSeedsFillsAnEmptyLibraryOnce(t *testing.T) {
	root := t.TempDir()
	if err := EnsureSeeds(root, "u1"); err != nil {
		t.Fatal(err)
	}
	cards, err := List(root, "u1")
	if err != nil || len(cards) != len(Seeds) {
		t.Fatalf("%+v %v", cards, err)
	}
	for i, s := range Seeds {
		if cards[i].Name != s.Name || cards[i].Body != s.Body {
			t.Fatalf("card %d is %+v, want %s", i, cards[i], s.Name)
		}
	}
	if cards[0].Name != "碎嘴群友" || cards[1].Name != "值班运维" || cards[2].Name != "周末搭子" {
		t.Fatalf("%+v", cards)
	}
	if err := EnsureSeeds(root, "u1"); err != nil {
		t.Fatal(err)
	}
	if again, _ := List(root, "u1"); len(again) != len(Seeds) {
		t.Fatalf("seeded twice: %d cards", len(again))
	}
	// An edited seed stays edited; deleting every seed does not bring them back.
	body := "改过的正文。"
	if _, err := Update(root, "u1", cards[0].ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	_ = EnsureSeeds(root, "u1")
	if got, _ := Get(root, "u1", cards[0].ID); got.Body != body {
		t.Fatalf("seed overwrote an edit: %q", got.Body)
	}
	for _, c := range cards {
		if err := Delete(root, "u1", c.ID); err != nil {
			t.Fatal(err)
		}
	}
	_ = EnsureSeeds(root, "u1")
	if left, _ := List(root, "u1"); len(left) != 0 {
		t.Fatalf("reseeded after delete: %+v", left)
	}
}

func TestEnsureSeedsLeavesAUsedLibraryUntouched(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, "u1", "我的卡", "只说一句。"); err != nil {
		t.Fatal(err)
	}
	// A library from before the seeds: a card, but no lock or marker yet. Nothing may be written.
	legacy := OwnerDir(root, "u2")
	if err := os.MkdirAll(filepath.Join(legacy, "0123456789abcdef"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "0123456789abcdef", "card.md"), []byte("name: 旧卡\n\n旧正文。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A library from before the marker: cards and a lock file. Nothing may be written either.
	if _, err := Create(root, "u4", "旧卡", "旧正文。"); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(OwnerDir(root, "u4"), seededFile))
	// A library whose cards were all deleted: the marker keeps the seeds out.
	emptied := OwnerDir(root, "u3")
	c, _ := Create(root, "u3", "x", "y")
	_ = Delete(root, "u3", c.ID)
	before := map[string][]string{}
	for _, d := range []string{OwnerDir(root, "u1"), legacy, emptied, OwnerDir(root, "u4")} {
		before[d] = names(t, d)
	}
	for _, o := range []string{"u1", "u2", "u3", "u4"} {
		if err := EnsureSeeds(root, o); err != nil {
			t.Fatal(err)
		}
	}
	for d, want := range before {
		if got := names(t, d); len(got) != len(want) {
			t.Fatalf("%s changed: %v -> %v", d, want, got)
		}
	}
	if cards, _ := List(root, "u1"); len(cards) != 1 || cards[0].Name != "我的卡" {
		t.Fatalf("%+v", cards)
	}
	if cards, _ := List(root, "u2"); len(cards) != 1 || cards[0].Name != "旧卡" {
		t.Fatalf("%+v", cards)
	}
	if cards, _ := List(root, "u3"); len(cards) != 0 {
		t.Fatalf("emptied library reseeded: %+v", cards)
	}
}

// A pick, edit or delete of a card that is not there takes the lock and fails;
// the library has still never had a card and must get the seeds.
func TestFailedCallsDoNotSkipSeeds(t *testing.T) {
	root := t.TempDir()
	missing, body := "0123456789abcdef", "x"
	if err := With(root, "u1", missing, func(Card) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := Update(root, "u1", missing, nil, &body); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := Delete(root, "u1", missing); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := Create(root, "u1", "x", "  "); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if err := EnsureSeeds(root, "u1"); err != nil {
		t.Fatal(err)
	}
	if cards, _ := List(root, "u1"); len(cards) != len(Seeds) {
		t.Fatalf("%d cards", len(cards))
	}
}

// A caller that arrives while a seed run is paused before its first card or between
// cards waits for the run, then lists every seed once.
func TestEnsureSeedsWaitsForARunningSeed(t *testing.T) {
	for pause := 0; pause < len(Seeds); pause++ {
		root := t.TempDir()
		paused, resume := make(chan struct{}), make(chan struct{})
		beforeSeed = func(i int) {
			if i == pause {
				close(paused)
				<-resume
			}
		}
		first := make(chan error, 1)
		go func() { first <- EnsureSeeds(root, "u1") }()
		select {
		case <-paused:
		case <-time.After(5 * time.Second):
			t.Fatalf("pause %d: the seed run never reached card %d", pause, pause)
		}
		got := make(chan int, 1)
		go func() {
			if err := EnsureSeeds(root, "u1"); err != nil {
				t.Error(err)
			}
			cards, _ := List(root, "u1")
			got <- len(cards)
		}()
		select {
		case n := <-got:
			t.Fatalf("pause %d: listed %d cards while the seed run was paused", pause, n)
		case <-time.After(100 * time.Millisecond):
		}
		close(resume)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if n := <-got; n != len(Seeds) {
			t.Fatalf("pause %d: listed %d cards", pause, n)
		}
		beforeSeed = nil
		if cards, _ := List(root, "u1"); len(cards) != len(Seeds) {
			t.Fatalf("pause %d: %d cards on disk", pause, len(cards))
		}
	}
}

func TestConcurrentEnsureSeedsSeedsOnce(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := EnsureSeeds(root, "u1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if cards, _ := List(root, "u1"); len(cards) != len(Seeds) {
		t.Fatalf("%d cards", len(cards))
	}
	if err := EnsureSeeds(root, "../x"); err == nil {
		t.Fatal("bad owner accepted")
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			out = append(out, p)
		}
		return nil
	})
	return out
}
