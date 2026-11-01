package bot

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAvatarChangeUploadStagesAndOneMiBBoundary(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "avatar", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, maxAvatar)
	copy(data, []byte("\x89PNG\r\n\x1a\n"))
	result, err := b.ApplyAvatarChange(data, false)
	if err != nil || result.Moved || result.Name != "avatar" || !reflect.DeepEqual(result.AppliedFields, []string{"avatar_file", "avatar_mime", "avatar"}) {
		t.Fatalf("upload: %+v %v", result, err)
	}
	stored, mime, ok, err := b.AvatarBytes()
	if err != nil || !ok || mime != "image/png" || !bytes.Equal(stored, data) {
		t.Fatalf("upload bytes: %d %q %v %v", len(stored), mime, ok, err)
	}
	info, err := os.Stat(b.AvatarPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v %v", info, err)
	}
}

func TestAvatarChangeValidationLeavesExistingResources(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "avatar", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	old := []byte("\x89PNG\r\n\x1a\nold")
	if _, err := b.ApplyAvatarChange(old, false); err != nil {
		t.Fatal(err)
	}
	oversized := make([]byte, maxAvatar+1)
	copy(oversized, old)
	for _, data := range [][]byte{[]byte("not an image"), oversized} {
		result, err := b.ApplyAvatarChange(data, false)
		if err == nil || len(result.AppliedFields) != 0 {
			t.Fatalf("invalid upload: %+v %v", result, err)
		}
	}
	if result, err := b.ApplyAvatarChange(old, true); err == nil || len(result.AppliedFields) != 0 {
		t.Fatalf("mixed action: %+v %v", result, err)
	}
	stored, err := os.ReadFile(b.AvatarPath())
	if err != nil || !bytes.Equal(stored, old) {
		t.Fatal("invalid request changed original upload", err)
	}
}

func TestAvatarChangeMIMEFailureReportsOnlySavedBytes(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "avatar", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(Dir(root, b.Name()), "config.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b.avatarMIMEPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.avatarMIMEPath(), "block"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	data := []byte("\x89PNG\r\n\x1a\nnew")
	result, err := b.ApplyAvatarChange(data, false)
	if err == nil || !reflect.DeepEqual(result.AppliedFields, []string{"avatar_file"}) {
		t.Fatalf("partial upload: %+v %v", result, err)
	}
	stored, err := os.ReadFile(b.AvatarPath())
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("first stage not reflected", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed upload published config", err)
	}
	if mode, _ := b.AvatarState(); mode != AvatarGenerated {
		t.Fatal("metadata snapshot changed before config commit")
	}
}

func TestAvatarChangeClearKeepsBestEffortCleanupSaved(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "avatar", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ApplyAvatarChange([]byte("\x89PNG\r\n\x1a\nold"), false); err != nil {
		t.Fatal(err)
	}
	avatarPath := b.AvatarPath()
	if err := os.Remove(avatarPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(avatarPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarPath, "block"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := b.ApplyAvatarChange(nil, true)
	if err != nil || !reflect.DeepEqual(result.AppliedFields, []string{"avatar"}) {
		t.Fatalf("clear metadata: %+v %v", result, err)
	}
	if mode, seed := b.AvatarState(); mode != AvatarGenerated || seed != "avatar" {
		t.Fatalf("clear snapshot: %q %q", mode, seed)
	}
	if _, _, ok, err := b.AvatarBytes(); err != nil || ok {
		t.Fatalf("leftover served as upload: %v %v", ok, err)
	}
}
