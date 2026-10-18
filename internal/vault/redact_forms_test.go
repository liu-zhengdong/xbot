package vault

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// A value inside a longer base64 stream (Basic auth "user:token") starts at
// any offset in a 3-byte group; its whole groups are still caught (#16).
func TestRedactUnalignedBase64(t *testing.T) {
	s := testStore(t)
	const secret = "tok-Zx8Qp2Lm9Vt4Rk7Wn3Hs"
	if err := s.Set("T", secret, []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, pre := range []string{"", "a", "ab", "user:", "alice:"} {
		for _, post := range []string{"", "x", "-and-more-bytes"} {
			for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
				blob := enc.EncodeToString([]byte(pre + secret + post))
				got := s.Redact("Authorization: Basic " + blob)
				if !strings.Contains(got, "[secret]") {
					t.Fatalf("%q+secret+%q: not redacted: %s", pre, post, got)
				}
				// At most two value bytes at each end may stay, as part of a
				// mixed group: no run of 8 encoded value characters is left.
				for skip := 0; skip < 3; skip++ {
					mid := enc.WithPadding(base64.NoPadding).EncodeToString([]byte(secret[skip:]))
					for i := 0; i+8 <= len(mid); i++ {
						if strings.Contains(got, mid[i:i+8]) && strings.Contains(blob, mid[i:i+8]) {
							t.Fatalf("%q+secret+%q: %q left in %s", pre, post, mid[i:i+8], got)
						}
					}
				}
			}
		}
	}
}

// base64 tools wrap at 76 (MIME) or 64 (PEM) columns; a form split by line
// breaks is still caught (#16).
func TestRedactWrappedBase64(t *testing.T) {
	s := testStore(t)
	var b strings.Builder
	for i := 0; b.Len() < 150; i++ {
		fmt.Fprintf(&b, "k%02d-Zx8Qp2Lm9Vt4Rk7Wn3", i)
	}
	secret := b.String()
	if err := s.Set("K", secret, []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{76, 64, 60} {
		for _, pre := range []string{"", "user:"} {
			enc := base64.StdEncoding.EncodeToString([]byte(pre + secret))
			var lines []string
			for len(enc) > width {
				lines = append(lines, enc[:width])
				enc = enc[width:]
			}
			lines = append(lines, enc)
			for _, nl := range []string{"\n", "\r\n"} {
				text := "$ base64 key\n" + strings.Join(lines, nl) + nl + "$ done\n"
				got := s.Redact(text)
				for _, ln := range lines[1 : len(lines)-1] {
					if strings.Contains(got, ln) {
						t.Fatalf("width %d %q: line %q left:\n%s", width, pre, ln, got)
					}
				}
				if !strings.HasPrefix(got, "$ base64 key\n") || !strings.HasSuffix(got, "$ done\n") || !strings.Contains(got, "[secret]") {
					t.Fatalf("width %d %q: %q", width, pre, got)
				}
			}
		}
	}
	// Ordinary wrapped base64 that holds no secret is left alone.
	other := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("unrelated bytes ", 12)))
	text := other[:76] + "\n" + other[76:]
	if got := s.Redact(text); got != text {
		t.Fatalf("unrelated block changed: %q", got)
	}
}

// One pass: an inserted [secret] is never scanned again, so a form that
// would match across it does not mangle the result (#17).
func TestRedactIsOnePass(t *testing.T) {
	s := testStore(t)
	if err := s.Set("A", "XYZ12345", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("B", "et]tail1", []string{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Redact("XYZ12345tail1"); got != "[secret]tail1" {
		t.Fatalf("got %q", got)
	}
}

// Equal-length forms come out in byte order every time (#17).
func TestFormsOrderIsStable(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 12; i++ {
		if err := s.Set(fmt.Sprintf("S%02d", i), fmt.Sprintf("v%02d-same-length", 11-i), []string{"example.com"}); err != nil {
			t.Fatal(err)
		}
	}
	forms := s.Forms()
	for i := 1; i < len(forms); i++ {
		a, b := forms[i-1], forms[i]
		if len(a) < len(b) || (len(a) == len(b) && a > b) {
			t.Fatalf("forms out of order at %d: %q before %q", i, a, b)
		}
	}
}
