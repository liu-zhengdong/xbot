package bot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAvatarSeedSaveFailureKeepsSnapshotAndUpload(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nold-upload")
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	// A non-file config makes the persistence seam fail deterministically.
	path := filepath.Join(Dir(root, "ada"), "config.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatarSeed("must-not-publish"); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if mode, seed := b.AvatarState(); mode != AvatarCustom || seed != "ada" {
		t.Fatalf("failed save published state: %s %s", mode, seed)
	}
	data, err := os.ReadFile(b.AvatarPath())
	if err != nil || !bytes.Equal(data, png) {
		t.Fatalf("failed save removed upload: %v", err)
	}
}

func TestAvatarSeedCleanupFailureStillReportsSaved(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A nonempty legacy avatar directory cannot be removed by the normal
	// best-effort cleanup. Generated metadata must remain authoritative.
	if err := os.Mkdir(b.AvatarPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.AvatarPath(), "leftover"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatarSeed("saved#8"); err != nil {
		t.Fatalf("saved metadata misreported as failure: %v", err)
	}
	fresh, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if mode, seed := fresh.AvatarState(); mode != AvatarGenerated || seed != "saved#8" {
		t.Fatalf("metadata did not save: %s %s", mode, seed)
	}
	if _, _, ok, err := fresh.AvatarBytes(); err != nil || ok {
		t.Fatalf("generated mode served leftover: %v %v", ok, err)
	}
}
