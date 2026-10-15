package clip

import (
	"strings"
	"testing"
)

func TestBytesRuneBoundary(t *testing.T) {
	s := strings.Repeat("你", 20)
	out, cut := Bytes(s, len(marker)+4)
	if !cut {
		t.Fatal("expected truncation")
	}
	if !utf8Valid(out) {
		t.Fatalf("invalid utf-8 %q", out)
	}
	if out[len(out)-len(marker):] != marker {
		t.Fatalf("missing marker: %q", out)
	}
}

func TestBytesNoCut(t *testing.T) {
	out, cut := Bytes("abc", 10)
	if cut || out != "abc" {
		t.Fatalf("got %q cut=%v", out, cut)
	}
}

func TestValid(t *testing.T) {
	out := Valid("a\xffb")
	if !utf8Valid(out) {
		t.Fatalf("still invalid: %q", out)
	}
}

func utf8Valid(s string) bool {
	return Valid(s) == s
}
