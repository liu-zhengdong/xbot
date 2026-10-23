package bot

import (
	"fmt"
	"sync"
	"testing"

	"xbot/internal/provider"
)

func TestConcurrentAppendUniqueSeqs(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 4
	const per = 200
	seqs := make([][]int, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			branch := fmt.Sprintf("bg_%d", w+1)
			for i := 0; i < per; i++ {
				e := Entry{
					Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(fmt.Sprintf("%s-%d", branch, i))},
					Branch:  branch,
				}
				seq, err := b.appendSeq(e)
				if err != nil {
					errs[w] = err
					return
				}
				seqs[w] = append(seqs[w], seq)
			}
		}(w)
	}
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", w, err)
		}
	}
	total := workers * per
	seen := make([]bool, total+1)
	for w, list := range seqs {
		if len(list) != per {
			t.Fatalf("worker %d appended %d", w, len(list))
		}
		for i, seq := range list {
			if seq < 1 || seq > total {
				t.Fatalf("worker %d seq %d", w, seq)
			}
			if seen[seq] {
				t.Fatalf("worker %d duplicate seq %d", w, seq)
			}
			seen[seq] = true
			if i > 0 && seq < list[i-1] {
				t.Fatalf("worker %d order %v", w, list)
			}
		}
	}
	for seq := 1; seq <= total; seq++ {
		if !seen[seq] {
			t.Fatalf("missing seq %d", seq)
		}
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != total {
		t.Fatalf("entries %d", len(entries))
	}
	// Each branch keeps the order its own goroutine wrote.
	next := map[string]int{}
	for _, e := range entries {
		want := fmt.Sprintf("%s-%d", e.Branch, next[e.Branch])
		if e.Message.Text() != want {
			t.Fatalf("branch %s got %q want %q", e.Branch, e.Message.Text(), want)
		}
		next[e.Branch]++
	}
	// Reopen and reindex from segment files: the same order must come back.
	CloseStore(b)
	b2, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := b2.Reindex(); err != nil {
		t.Fatal(err)
	}
	again, err := b2.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(entries) {
		t.Fatalf("reopen entries %d vs %d", len(again), len(entries))
	}
	for i := range entries {
		if entries[i].Message.Text() != again[i].Message.Text() || entries[i].Branch != again[i].Branch {
			t.Fatalf("reopen row %d %+v vs %+v", i, entries[i], again[i])
		}
	}
}

func TestAppendForkValidation(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 0, "task"); err == nil {
		t.Fatal("from 0 accepted")
	}
	if _, err := b.AppendFork("main", 1, "task"); err == nil {
		t.Fatal("bad branch accepted")
	}
	if _, err := b.AppendFork("bg_7", 1, "task"); err == nil {
		t.Fatal("from past the end accepted")
	}
	if err := b.Append(mainRow("start")); err != nil {
		t.Fatal(err)
	}
	seq, err := b.AppendFork("bg_7", 1, "long job")
	if err != nil || seq != 2 {
		t.Fatalf("fork seq %d %v", seq, err)
	}
	if _, err := b.AppendForkEnd("bg_7", "nope", 1); err == nil {
		t.Fatal("bad status accepted")
	}
	if _, err := b.AppendForkEnd("bg_7", ForkDone, -1); err == nil {
		t.Fatal("negative steps accepted")
	}
	seq, err = b.AppendForkEnd("bg_7", ForkInterrupted, 4)
	if err != nil || seq != 3 {
		t.Fatalf("fork end seq %d %v", seq, err)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Mark == nil || entries[1].Mark.Kind != ForkKindStart || entries[1].Mark.From != 1 {
		t.Fatalf("fork entry %+v", entries)
	}
	if entries[2].Mark == nil || entries[2].Mark.Kind != ForkKindEnd || entries[2].Mark.Status != ForkInterrupted || entries[2].Mark.Steps != 4 {
		t.Fatalf("fork end entry %+v", entries[2])
	}
}
