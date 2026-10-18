package appshell

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestDefaults(t *testing.T) {
	opts := defaults(Options{Title: "XBot", URL: "http://localhost/"})
	if opts.Width != 1100 || opts.Height != 720 || opts.Title != "XBot" || opts.URL != "http://localhost/" {
		t.Fatal(opts)
	}
	custom := Options{Width: 800, Height: 600}
	if defaults(custom) != custom {
		t.Fatal("explicit size changed")
	}
}

// The page sends its theme; the native window buttons must follow it, not the OS.
func TestThemeSource(t *testing.T) {
	for theme, want := range map[string]mygo.ThemeSource{
		"light":  mygo.ThemeLight,
		"dark":   mygo.ThemeDark,
		"system": mygo.ThemeSystem,
		"":       mygo.ThemeSystem,
	} {
		got, err := themeSource(theme)
		if err != nil || got != want {
			t.Errorf("themeSource(%q) = %v, %v; want %v", theme, got, err, want)
		}
	}
	for _, bad := range []string{"sepia", "Light", "auto"} {
		if _, err := themeSource(bad); err == nil {
			t.Errorf("themeSource(%q) accepted an unknown theme", bad)
		}
	}
}
