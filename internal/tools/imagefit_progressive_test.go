package tools

import (
	"bytes"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

// progressiveJPEG encodes a baseline JPEG and flips its first SOF0 marker to
// SOF2 so the header reads as progressive. The stdlib encoder cannot write a
// progressive scan, and the resulting file does not decode; only the header
// estimate and the cap decision are under test.
func progressiveJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	i := bytes.Index(b, []byte{0xFF, 0xC0})
	if i < 0 {
		t.Fatal("no SOF0 marker")
	}
	out := append([]byte(nil), b...)
	out[i+1] = 0xC2
	return out
}

func TestProgressiveJPEGPhotoPassesCap(t *testing.T) {
	photo := progressiveJPEG(t, 4032, 3024)
	w, h, decoded, ok := imageMeta("image/jpeg", photo)
	if !ok || w != 4032 || h != 3024 {
		t.Fatalf("%dx%d ok=%v", w, h, ok)
	}
	if decoded != int64(4032)*int64(3024)*12 || decoded > maxDecodedBytes {
		t.Fatalf("estimate %d cap %d", decoded, maxDecodedBytes)
	}
	// The patched file cannot be decoded, so it may fail as undecodable, but
	// it must not be refused by the cap.
	if _, _, err := fitImage("image/jpeg", photo); err != nil && strings.Contains(err.Error(), "pixel cap") {
		t.Fatal(err)
	}

	huge := progressiveJPEG(t, 4096, 4096)
	_, _, decoded, ok = imageMeta("image/jpeg", huge)
	if !ok || decoded != int64(4096)*int64(4096)*12 || decoded <= maxDecodedBytes {
		t.Fatalf("4096 estimate %d cap %d", decoded, maxDecodedBytes)
	}
	if _, _, err := fitImage("image/jpeg", huge); err == nil || !strings.Contains(err.Error(), "pixel cap") {
		t.Fatal(err)
	}
}

func TestDecodeCapAllows16BitRGBAAndRefusesOversizeSide(t *testing.T) {
	rgba16 := widePNG(4096, 4096, 16)
	_, _, decoded, ok := imageMeta("image/png", rgba16)
	if !ok || decoded != int64(4096)*int64(4096)*8 || decoded > maxDecodedBytes {
		t.Fatalf("16-bit RGBA estimate %d cap %d", decoded, maxDecodedBytes)
	}
	// Header-only, so the decode fails; the cap must not be what refuses it.
	if _, _, err := fitImage("image/png", rgba16); err == nil || strings.Contains(err.Error(), "pixel cap") {
		t.Fatalf("16-bit RGBA: %v", err)
	}
	wide := widePNG(4097, 100, 8)
	if _, _, decoded, ok := imageMeta("image/png", wide); !ok || decoded > maxDecodedBytes {
		t.Fatalf("oversize side estimate %d ok=%v", decoded, ok)
	}
	if _, _, err := fitImage("image/png", wide); err == nil || !strings.Contains(err.Error(), "pixel cap") {
		t.Fatal(err)
	}
}
