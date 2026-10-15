package web

import (
	"bytes"
	"os"
	"testing"
)

func TestDefaultAvatarIsSoftBlob(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	html, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(js, []byte("ellipse")) || bytes.Contains(html, []byte("ellipse")) {
		t.Fatal("default avatar still draws a face")
	}
	if !bytes.Contains(js, []byte("BLOBS")) || !bytes.Contains(js, []byte("markInitial")) || !bytes.Contains(js, []byte("blobFill")) {
		t.Fatal("soft blob avatar is missing")
	}
	if bytes.Contains(css, []byte(".avatar {")) && bytes.Contains(css, []byte("border-radius: 50%")) {
		// A hard circle on the avatar box would clip the blob back into a smiley-less coin.
		// Uploaded photos keep their own radius. The default mark must not.
		if !bytes.Contains(css, []byte(".avatar.has-photo")) {
			t.Fatal("avatar radius is not limited to photos")
		}
	}
	doc, err := os.ReadFile("../../docs/DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doc, []byte("软边圆斑")) || !bytes.Contains(doc, []byte("没有五官")) {
		t.Fatal("DESIGN.md does not describe the blob avatar")
	}
}
