package web

import (
	"testing"
	"unicode/utf8"
)

func TestClipRunes(t *testing.T) {
	for _, tc := range []struct {
		text string
		n    int
		want string
	}{
		{"  中文任务标题  ", 4, "中文任务…"},
		{"中文", 2, "中文"},
		{"a😀中𠮷文", 4, "a😀中𠮷…"},
		{" abc ", 2, "ab…"},
		{"中文", 0, "…"},
		{"  ", 3, ""},
	} {
		got := clip(tc.text, tc.n)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q; want %q", tc.text, tc.n, got, tc.want)
		}
	}
}
