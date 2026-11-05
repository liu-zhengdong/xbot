package web

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"xbot/internal/bot"
)

// An upload keeps the same URL, so browsers would reuse the old picture.
// paintBot therefore appends a version derived from the file's change time.
func TestPaintBotVersionFollowsUpload(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAvatar(buf.Bytes(), "image/png"); err != nil {
		t.Fatal(err)
	}
	first, _, _, _ := paintBot(b)
	if len(first) <= len("/api/bots/ada/avatar") || first[:len("/api/bots/ada/avatar")] != "/api/bots/ada/avatar" {
		t.Fatalf("url %q", first)
	}
	if first == "/api/bots/ada/avatar" {
		t.Fatalf("uploaded avatar has no version: %q", first)
	}

	later := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(b.AvatarPath(), later, later); err != nil {
		t.Fatal(err)
	}
	second, _, _, _ := paintBot(b)
	if second == first {
		t.Fatalf("version did not follow the new upload: %q", second)
	}

	// A Bot without an upload keeps the plain path, so nothing else changes.
	plain, err := bot.Create(root, "bob", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if url, _, _, _ := paintBot(plain); url != "" {
		t.Fatalf("generated avatar should have no url, got %q", url)
	}
}
