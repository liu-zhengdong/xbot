package web

import (
	"bytes"
	"testing"
)

func TestCodingCardInUI(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(js, []byte("coding_card")) || !bytes.Contains(css, []byte("coding-card")) {
		t.Fatal("coding card is not in the web ui")
	}
}
