package personacard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/fsutil"
)

func seedBody(name string) string {
	for _, s := range Seeds {
		if s.Name == name {
			return s.Body
		}
	}
	return ""
}

func thinSeedLibrary(t *testing.T) (string, []Card) {
	t.Helper()
	root := t.TempDir()
	var cards []Card
	for _, s := range Seeds {
		if legacySeedBodies[s.Name] == "" {
			continue
		}
		c, err := Create(root, s.Name, legacySeedBodies[s.Name])
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	if !has(Dir(root), seededFile) {
		t.Fatal("fixture must represent an already seeded library")
	}
	return root, cards
}

func TestEnsureSeedsUpgradesOnlyUnchangedThinCards(t *testing.T) {
	for _, edit := range []string{"none", "one_body_character", "title", "trimmed_body"} {
		t.Run(edit, func(t *testing.T) {
			root, cards := thinSeedLibrary(t)
			first := cards[0]
			var err error
			switch edit {
			case "one_body_character":
				body := first.Body + "改"
				first, err = Update(root, first.ID, nil, &body)
			case "title":
				name := first.Name + "（我的）"
				first, err = Update(root, first.ID, &name, nil)
			case "trimmed_body":
				// read trims the blank separator and trailing whitespace on disk.
				err = os.WriteFile(filepath.Join(Dir(root), first.ID, "card.md"),
					[]byte("name: "+first.Name+"\n\n \n"+first.Body+"\n \t\n"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureSeeds(root); err != nil {
				t.Fatal(err)
			}
			for i, c := range cards {
				got, err := Get(root, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 && (edit == "one_body_character" || edit == "title") {
					if got != first {
						t.Fatalf("edited card changed: %+v", got)
					}
				} else if got.Name != c.Name || got.Body != seedBody(c.Name) {
					t.Fatalf("%s was not upgraded to its current seed", c.Name)
				}
			}
			before := snapshotLibrary(t, Dir(root))
			if err := EnsureSeeds(root); err != nil {
				t.Fatal(err)
			}
			if after := snapshotLibrary(t, Dir(root)); !reflect.DeepEqual(before, after) {
				t.Fatal("repeated ensure changed contents, paths, modes or mtimes")
			}
		})
	}
}

func TestEnsureSeedsUpgradeWaitsForLibraryLock(t *testing.T) {
	root, cards := thinSeedLibrary(t)
	dir := Dir(root)
	lock, err := fsutil.Lock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	done := make(chan error, 1)
	go func() { done <- EnsureSeeds(root) }()
	select {
	case err := <-done:
		t.Fatalf("ensure returned before the library lock was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// Simulate an edit finishing in another process while it owns the lock.
	const edited = "用户刚改过的正文"
	if _, err := write(dir, cards[0].ID, cards[0].Name, edited); err != nil {
		t.Fatal(err)
	}
	lock.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ensure did not finish after releasing the library lock")
	}
	for i, c := range cards {
		want := seedBody(c.Name)
		if i == 0 {
			want = edited
		}
		if got, err := Get(root, c.ID); err != nil || got.Body != want {
			t.Fatalf("card %d: lost edit or missed upgrade: %v", i, err)
		}
	}
}

func TestEnsureSeedsUpgradeContinuesAfterWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires directory write permissions to be enforced")
	}
	root, cards := thinSeedLibrary(t)
	// The newest card is visited first by list; make that card unwritable.
	failed := cards[len(cards)-1]
	dir := filepath.Join(Dir(root), failed.ID)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := EnsureSeeds(root); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("want write error, got %v", err)
	}
	for i, c := range cards {
		want := seedBody(c.Name)
		if c.ID == failed.ID {
			want = c.Body
		}
		if got, err := Get(root, c.ID); err != nil || got.Body != want {
			t.Fatalf("card %d: failure blocked other cards or damaged old body: %v", i, err)
		}
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	if got, err := Get(root, failed.ID); err != nil || got.Body != seedBody(failed.Name) {
		t.Fatalf("failed card was not upgraded on retry: %v", err)
	}
}

func TestEnsureSeedsFillsAnEmptyLibraryOnce(t *testing.T) {
	root := t.TempDir()
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	cards, err := List(root)
	if err != nil || len(cards) != len(Seeds) {
		t.Fatalf("%+v %v", cards, err)
	}
	for i, s := range Seeds {
		if cards[i].Name != s.Name || cards[i].Body != s.Body {
			t.Fatalf("card %d is %+v, want %s", i, cards[i], s.Name)
		}
	}
	if cards[0].Name != DefaultName || cards[1].Name != "碎嘴群友" || cards[2].Name != "值班运维" || cards[3].Name != "周末搭子" {
		t.Fatalf("%+v", cards)
	}
	if def, ok := Default(root); !ok || def.ID != cards[0].ID {
		t.Fatalf("%+v", cards)
	}
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	if again, _ := List(root); len(again) != len(Seeds) {
		t.Fatalf("seeded twice: %d cards", len(again))
	}
	// An edited seed stays edited; deleting every seed does not bring them back.
	body := "改过的正文。"
	if _, err := Update(root, cards[1].ID, nil, &body); err != nil {
		t.Fatal(err)
	}
	_ = EnsureSeeds(root)
	if got, _ := Get(root, cards[1].ID); got.Body != body {
		t.Fatalf("seed overwrote an edit: %q", got.Body)
	}
	// The default card stays: deleting it is refused, so bots always have one to fall back to.
	if err := Delete(root, cards[0].ID); !errors.Is(err, ErrDefault) {
		t.Fatalf("deleted the default card: %v", err)
	}
	for _, c := range cards[1:] {
		if err := Delete(root, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	_ = EnsureSeeds(root)
	if left := userCards(t, root); len(left) != 0 {
		t.Fatalf("reseeded after delete: %+v", left)
	}
}

func TestEnsureSeedsLeavesAUsedLibraryUntouched(t *testing.T) {
	used := t.TempDir()
	if _, err := Create(used, "我的卡", "只说一句。"); err != nil {
		t.Fatal(err)
	}
	// A library from before the seeds: a card, but no lock or marker yet. Nothing may be written.
	legacy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(Dir(legacy), "0123456789abcdef"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(legacy), "0123456789abcdef", "card.md"), []byte("name: 旧卡\n\n旧正文。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A library from before the marker: cards and a lock file. Nothing may be written either.
	unmarked := t.TempDir()
	if _, err := Create(unmarked, "旧卡", "旧正文。"); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(Dir(unmarked), seededFile))
	// A library whose cards were all deleted: the marker keeps the seeds out.
	emptied := t.TempDir()
	c, _ := Create(emptied, "x", "y")
	_ = Delete(emptied, c.ID)
	roots := []string{used, legacy, unmarked, emptied}
	before := map[string]map[string]libraryEntry{}
	for _, r := range roots {
		before[r] = snapshotLibrary(t, Dir(r))
	}
	for _, r := range roots {
		if err := EnsureSeeds(r); err != nil {
			t.Fatal(err)
		}
	}
	for r, want := range before {
		d := Dir(r)
		if got := withoutDefault(snapshotLibrary(t, d), d); !reflect.DeepEqual(got, withoutDefault(want, d)) {
			t.Fatalf("%s changed: %v -> %v", d, want, got)
		}
	}
	if cards := userCards(t, used); len(cards) != 1 || cards[0].Name != "我的卡" {
		t.Fatalf("%+v", cards)
	}
	if cards := userCards(t, legacy); len(cards) != 1 || cards[0].Name != "旧卡" {
		t.Fatalf("%+v", cards)
	}
	if cards := userCards(t, emptied); len(cards) != 0 {
		t.Fatalf("emptied library reseeded: %+v", cards)
	}
}

// A pick, edit or delete of a card that is not there takes the lock and fails;
// the library has still never had a card and must get the seeds.
func TestFailedCallsDoNotSkipSeeds(t *testing.T) {
	root := t.TempDir()
	missing, body := "0123456789abcdef", "x"
	if err := With(root, missing, func(Card) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := Update(root, missing, nil, &body); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := Delete(root, missing); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := Create(root, "x", "  "); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	if cards, _ := List(root); len(cards) != len(Seeds) {
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
		go func() { first <- EnsureSeeds(root) }()
		select {
		case <-paused:
		case <-time.After(5 * time.Second):
			t.Fatalf("pause %d: the seed run never reached card %d", pause, pause)
		}
		got := make(chan int, 1)
		go func() {
			if err := EnsureSeeds(root); err != nil {
				t.Error(err)
			}
			cards, _ := List(root)
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
		if cards, _ := List(root); len(cards) != len(Seeds) {
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
			if err := EnsureSeeds(root); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if cards, _ := List(root); len(cards) != len(Seeds) {
		t.Fatalf("%d cards", len(cards))
	}
}

// A library from before the marker (cards, no .seeded, with or without .lock) lists with
// no writes; once its owner deletes every card it stays empty.
func TestLegacyLibraryEmptiedStaysEmpty(t *testing.T) {
	// A fresh process has no library state in memory; the marker must suffice.
	if root := os.Getenv("XBOT_TEST_EMPTIED_CARD_ROOT"); root != "" {
		if err := EnsureSeeds(root); err != nil {
			t.Fatal(err)
		}
		if cards := userCards(t, root); len(cards) != 0 {
			t.Fatalf("reopened library: %+v", cards)
		}
		return
	}
	for _, withLock := range []bool{true, false} {
		t.Run(map[bool]string{true: "with_lock", false: "without_lock"}[withLock], func(t *testing.T) {
			root := t.TempDir()
			dir := Dir(root)
			for _, id := range []string{"0123456789abcdef", "fedcba9876543210"} {
				if err := os.MkdirAll(filepath.Join(dir, id), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, id, "card.md"), []byte("name: 旧卡"+id[:2]+"\n\n旧正文。\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if withLock {
				if err := os.WriteFile(filepath.Join(dir, ".lock"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotLibrary(t, dir)
			if err := EnsureSeeds(root); err != nil {
				t.Fatal(err)
			}
			if got := withoutDefault(snapshotLibrary(t, dir), dir); !reflect.DeepEqual(got, withoutDefault(before, dir)) {
				t.Fatalf("lock %v: list wrote %v -> %v", withLock, before, got)
			}
			cards := userCards(t, root)
			if len(cards) != 2 {
				t.Fatalf("legacy cards: %+v", cards)
			}
			for _, c := range cards {
				if err := Delete(root, c.ID); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 3; i++ {
				if err := EnsureSeeds(root); err != nil {
					t.Fatal(err)
				}
				if left := userCards(t, root); len(left) != 0 {
					t.Fatalf("lock %v: reseeded after deleting every old card: %+v", withLock, left)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, seededFile)); err != nil {
				t.Fatalf("deleting legacy cards did not persist history: %v", err)
			}
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "-test.run=^TestLegacyLibraryEmptiedStaysEmpty$", "-test.v")
			cmd.Env = append(os.Environ(), "XBOT_TEST_EMPTIED_CARD_ROOT="+root)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("reopen in a new process: %v\n%s", err, out)
			}
		})
	}
}

type libraryEntry struct {
	Mode    os.FileMode
	ModTime time.Time
	Body    string
}

// Compare contents and mtimes as well as paths: equal file counts do not prove
// that listing an existing library made no writes.
// withoutDefault drops what ensureDefault may add to any library: the default
// card, the file naming it, the lock it takes, and the library directory's own mtime.
func withoutDefault(snap map[string]libraryEntry, dir string) map[string]libraryEntry {
	raw, _ := os.ReadFile(filepath.Join(dir, defaultFile))
	id := strings.TrimSpace(string(raw))
	out := map[string]libraryEntry{}
	for path, e := range snap {
		if path == dir || path == filepath.Join(dir, defaultFile) || path == filepath.Join(dir, ".lock") || (id != "" && strings.HasPrefix(path, filepath.Join(dir, id))) {
			continue
		}
		out[path] = e
	}
	return out
}

// userCards lists the library's cards other than the default one.
func userCards(t *testing.T, root string) []Card {
	t.Helper()
	cards, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	def, ok := Default(root)
	if !ok || def.Name != DefaultName {
		t.Fatalf("no default card: %+v", def)
	}
	var out []Card
	for _, c := range cards {
		if c.ID != def.ID {
			out = append(out, c)
		}
	}
	return out
}

func snapshotLibrary(t *testing.T, dir string) map[string]libraryEntry {
	t.Helper()
	out := map[string]libraryEntry{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		entry := libraryEntry{Mode: info.Mode(), ModTime: info.ModTime()}
		if !info.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry.Body = string(body)
		}
		out[path] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeleteWithFailedCallbackKeepsLegacyCard(t *testing.T) {
	root := t.TempDir()
	c, err := Create(root, "旧卡", "旧正文。")
	if err != nil {
		t.Fatal(err)
	}
	dir := Dir(root)
	if err := os.Remove(filepath.Join(dir, seededFile)); err != nil {
		t.Fatal(err)
	}
	want := snapshotLibrary(t, dir)
	denied := errors.New("still in use")
	if err := DeleteWith(root, c.ID, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("delete: %v", err)
	}
	if got := snapshotLibrary(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed callback changed legacy library: %v -> %v", want, got)
	}
}

func TestDeleteKeepsCardIfSeedMarkerCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Chmod does not enforce directory write permissions")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	root := t.TempDir()
	c, err := Create(root, "旧卡", "旧正文。")
	if err != nil {
		t.Fatal(err)
	}
	dir := Dir(root)
	if err := os.Remove(filepath.Join(dir, seededFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := Delete(root, c.ID); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("delete without writable history: %v", err)
	}
	if _, err := Get(root, c.ID); err != nil {
		t.Fatalf("card lost when history could not be persisted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, seededFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected history after failed write: %v", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Delete(root, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSeeds(root); err != nil {
		t.Fatal(err)
	}
	if cards := userCards(t, root); len(cards) != 0 {
		t.Fatalf("reseeded after retrying delete: %+v", cards)
	}
}
