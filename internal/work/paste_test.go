package work

import "testing"

func TestPastedNameForIsStableAndValid(t *testing.T) {
	a, err := PastedNameFor("image/png", []byte("x"))
	if err != nil || !ValidPastedID(a) {
		t.Fatalf("%q %v", a, err)
	}
	if b, _ := PastedNameFor("image/png", []byte("x")); b != a {
		t.Fatalf("same bytes, two names: %s %s", a, b)
	}
	if c, _ := PastedNameFor("image/png", []byte("y")); c == a {
		t.Fatal("different bytes, one name")
	}
	if _, err := PastedNameFor("text/plain", []byte("x")); err == nil {
		t.Fatal("non-image named")
	}
}
