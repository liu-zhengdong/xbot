package appshell

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestWindowStateStore(t *testing.T) {
	f := fileStore{filepath.Join(t.TempDir(), "window.json")}
	if got, _ := f.LoadWindowStates(); len(got) != 0 {
		t.Fatal(got)
	}
	want := mygo.WindowState{X: -1200, Y: 40, Width: 1200, Height: 800, Maximized: true}
	if err := f.SaveWindowStates(map[string]mygo.WindowState{stateKey: want, "other": want}); err != nil {
		t.Fatal(err)
	}
	got, err := f.LoadWindowStates()
	if err != nil || len(got) != 1 || got[stateKey] != want {
		t.Fatal(got, err)
	}
	if info, err := os.Stat(f.path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	for _, data := range []string{
		`{`, `{"main":{"width":899,"height":720}}`, `{"main":{"width":1100,"height":559}}`,
		`{"main":{"width":10001,"height":720}}`, `{"main":{"width":1100,"height":720}} {}`,
	} {
		if err := os.WriteFile(f.path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if got, _ := f.LoadWindowStates(); len(got) != 0 {
			t.Fatalf("%s: %+v", data, got)
		}
	}
}
