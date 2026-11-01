package bot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrimaryCommitRuntimeStages(t *testing.T) {
	for _, tc := range []struct {
		name, target, stage string
		obstructPointer     bool
		wantNotFound        bool
	}{
		{name: "invalid", target: "../invalid", stage: "validation"},
		{name: "missing", target: "missing", stage: "target", wantNotFound: true},
		{name: "pointer", target: "target", stage: "primary", obstructPointer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := Create(root, "target", "duties", nil); err != nil {
				t.Fatal(err)
			}
			if tc.obstructPointer {
				if err := os.Mkdir(primaryPath(root), 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := SetPrimary(root, tc.target)
			var committed *PrimaryCommitError
			if !errors.As(err, &committed) || committed.Applied || committed.Stage != tc.stage {
				t.Fatalf("uncommitted stage: %#v %v", committed, err)
			}
			if err.Error() != committed.Err.Error() || !errors.Is(err, committed.Err) {
				t.Fatalf("original cause/message lost: %v cause %v", err, committed.Err)
			}
			if tc.wantNotFound && !errors.Is(err, ErrNotFound) {
				t.Fatalf("ErrNotFound not preserved: %v", err)
			}
		})
	}
	t.Run("lock entrance remains plain", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Create(root, "target", "duties", nil); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(primaryLock(root), 0700); err != nil {
			t.Fatal(err)
		}
		err := SetPrimary(root, "target")
		var committed *PrimaryCommitError
		if err == nil || errors.As(err, &committed) {
			t.Fatalf("lock entrance should remain its original error: %v", err)
		}
	})
}

func TestPrimaryCommitRuntimeLatePinAndSamePrimary(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"old", "target"} {
		if _, err := Create(root, name, "duties", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetPrimary(root, "old"); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(Dir(root, "target"), "config.json.lock")
	if err := os.Rename(lock, lock+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	// A normal config has no legacy style migration: Load succeeds, the actual
	// primary pointer is written, and only SetPinned's config lock then fails.
	for _, label := range []string{"transfer", "same primary"} {
		err := SetPrimary(root, "target")
		var committed *PrimaryCommitError
		var pathErr *os.PathError
		if !errors.As(err, &committed) || !committed.Applied || committed.Stage != "pin" || !errors.As(err, &pathErr) || pathErr.Path != lock {
			t.Fatalf("%s actual late config failure: %#v %v", label, committed, err)
		}
		if err.Error() != pathErr.Error() || !errors.Is(err, pathErr.Err) {
			t.Fatalf("cause/message changed: %v cause %v", err, pathErr.Err)
		}
		if primary, err := Primary(root); err != nil || primary != "target" {
			t.Fatalf("pointer must already be target: %q %v", primary, err)
		}
		target, err := Load(root, "target")
		if err != nil || target.Config().Pinned {
			t.Fatalf("failed config save must leave durable pin false: %v", err)
		}
	}
	// Remove only the empty obstruction in this test's known target lock path.
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := SetPrimary(root, "target"); err != nil {
		t.Fatal(err)
	}
	target, err := Load(root, "target")
	if err != nil || !target.Config().Pinned {
		t.Fatalf("same primary must ensure pin: %v", err)
	}
	old, err := Load(root, "old")
	if err != nil || !old.Config().Pinned {
		t.Fatalf("transfer must preserve old primary pin: %v", err)
	}
}
