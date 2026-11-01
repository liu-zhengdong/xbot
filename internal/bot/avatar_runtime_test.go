package bot

import (
	"bytes"
	"os"
	"sync"
	"testing"
)

func TestAvatarSeedSurvivesStaleConfigSave(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetAvatarSeed("new-seed#3"); err != nil {
		t.Fatal(err)
	}
	if err := stale.SetPinned(true); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if avatar, seed := fresh.AvatarState(); avatar != AvatarGenerated || seed != "new-seed#3" {
		t.Fatalf("stale save rolled back avatar: %s %s", avatar, seed)
	}
}

func TestAvatarConcurrentReadersAndWriters(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 20; i++ {
			if err := b.SetAvatar(png, "image/png"); err != nil {
				errors <- err
				return
			}
			if err := b.ClearAvatar(); err != nil {
				errors <- err
				return
			}
			if err := b.SetAvatarSeed("rotated#3"); err != nil {
				errors <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 40; i++ {
			_ = b.Config()
			_, _ = b.AvatarState()
			// Reads may race with a legitimate mode change; race detection verifies
			// memory safety while final-state behavior is checked separately.
			_, _, _, _ = b.AvatarBytes()
			_, _ = ExportTemplate(b)
		}
	}()
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func TestGeneratedAvatarDoesNotServeLeftoverUpload(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nleftover")
	if err := b.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatarSeed("generated#4"); err != nil {
		t.Fatal(err)
	}
	// Represents best-effort cleanup failing after generated metadata saved.
	if err := os.WriteFile(b.AvatarPath(), png, 0600); err != nil {
		t.Fatal(err)
	}
	data, _, ok, err := b.AvatarBytes()
	if err != nil || ok || bytes.Equal(data, png) {
		t.Fatalf("generated mode served leftover upload: ok=%v err=%v", ok, err)
	}
}
