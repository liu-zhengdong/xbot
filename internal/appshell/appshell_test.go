package appshell

import "testing"

func TestDefaults(t *testing.T) {
	opts := defaults(Options{Title: "XBot", URL: "http://localhost/"})
	if opts.Width != 1100 || opts.Height != 760 || opts.Title != "XBot" || opts.URL != "http://localhost/" {
		t.Fatal(opts)
	}
	custom := Options{Width: 800, Height: 600}
	if defaults(custom) != custom {
		t.Fatal("explicit size changed")
	}
}
