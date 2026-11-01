package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClearAvatarKeepsFreshSeedAcrossHandles(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetAvatarSeed("newest#7"); err != nil {
		t.Fatal(err)
	}
	if err := b.ClearAvatar(); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if mode, seed := fresh.AvatarState(); mode != AvatarGenerated || seed != "newest#7" {
		t.Fatalf("stale clear replaced seed: %s %s", mode, seed)
	}
}

func TestStaleCustomHandleDoesNotServeGeneratedLeftover(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nleftover")
	if err := a.SetAvatar(png, "image/png"); err != nil {
		t.Fatal(err)
	}
	stale, err := Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetAvatarSeed("generated#7"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.AvatarPath(), png, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := stale.AvatarBytes(); err != nil || ok {
		t.Fatalf("old custom handle served generated leftover: ok=%v err=%v", ok, err)
	}
	tpl, err := ExportTemplate(stale)
	if err != nil || tpl.Avatar != "" || tpl.AvatarSeed != "generated#7" {
		t.Fatalf("template used stale custom state: %+v %v", tpl, err)
	}
}

func TestRenamePinsLegacyCustomSeedWithoutChangingMode(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "duty", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatar([]byte("\x89PNG\r\n\x1a\nlegacy"), "image/png"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(Dir(root, "ada"), "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	delete(cfg, "avatar_seed")
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, "ada", "notes"); err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(root, "notes")
	if err != nil {
		t.Fatal(err)
	}
	if mode, seed := fresh.AvatarState(); mode != AvatarCustom || seed != "ada" {
		t.Fatalf("legacy rename changed mode/face: %s %s", mode, seed)
	}
}
