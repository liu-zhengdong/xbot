package machine

import (
	"os"
	"testing"
)

func TestDefaultAllowAndSet(t *testing.T) {
	root := t.TempDir()
	mode, err := Get(root, Local)
	if err != nil || mode != Allow {
		t.Fatalf("%s %v", mode, err)
	}
	if err := Set(root, Local, Ask); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(Path(root))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	mode, err = Get(root, "")
	if err != nil || mode != Ask {
		t.Fatalf("empty name %s %v", mode, err)
	}
	if err := Set(root, "desk", Off); err != nil {
		t.Fatal(err)
	}
	mode, err = Get(root, "desk")
	if err != nil || mode != Off {
		t.Fatalf("%s %v", mode, err)
	}
	if err := Set(root, Local, "open"); err == nil {
		t.Fatal("bad mode accepted")
	}
	if err := Set(root, "bad name", Allow); err == nil {
		t.Fatal("bad name accepted")
	}
}
