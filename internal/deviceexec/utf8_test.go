package deviceexec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDetailPreservesUTF8AcrossOutputPages(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	text := strings.Repeat("a", 4095) + "你tail"
	submit(t, m, s, testCall("utf8", "exec", "sh", map[string]any{"command": "printf '" + text + "'"}))
	waitDetail(t, m, s, "utf8")
	first, err := m.Detail(s, "utf8", 0, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if first.NextOffset != 4095 || len(first.Output) != 4095 || !utf8.ValidString(first.Output) {
		t.Fatalf("first page: bytes=%d next=%d", len(first.Output), first.NextOffset)
	}
	second, err := m.Detail(s, "utf8", first.NextOffset, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if first.Output+second.Output != text || second.NextOffset != int64(len(text)) {
		t.Fatalf("UTF-8 lost across pages: second=%q next=%d", second.Output, second.NextOffset)
	}
	if _, err := m.Detail(s, "utf8", 4095, 2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short limit swallowed character: %v", err)
	}
	if _, err := m.Detail(s, "utf8", 4096, 4096); !errors.Is(err, ErrInvalid) {
		t.Fatalf("continuation offset accepted: %v", err)
	}
	whole, err := m.Detail(s, "utf8", 4095, 3)
	if err != nil || whole.Output != "你" || whole.NextOffset != 4098 {
		t.Fatalf("exact character page: %+v %v", whole, err)
	}
}

func TestDetailNormalizesBinaryLogsWithoutLosingByteOffsets(t *testing.T) {
	requireShell(t)
	m := testManager(t)
	s := testSource()
	raw := append([]byte{'a', 0x80, 0xff}, []byte("你tail🙂")...)
	raw = append(raw, 0xc3, 0x28, 0xa1, 'z')
	path := filepath.Join(m.Root, "binary-output")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	submit(t, m, s, testCall("binary-utf8", "exec", "sh", map[string]any{"command": "head -c 100000 '" + path + "'"}))
	waitDetail(t, m, s, "binary-utf8")
	var text strings.Builder
	offset := int64(0)
	for offset < int64(len(raw)) {
		detail, err := m.Detail(s, "binary-utf8", offset, 8)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(detail.Output) || len(detail.Output) > 8 || detail.NextOffset <= offset || detail.NextOffset > int64(len(raw)) {
			t.Fatalf("invalid binary page: %+v", detail)
		}
		text.WriteString(detail.Output)
		offset = detail.NextOffset
	}
	if text.String() != string([]rune(string(raw))) {
		t.Fatalf("binary normalization lost bytes: %q", text.String())
	}
	standalone, err := m.Detail(s, "binary-utf8", 1, 3)
	if err != nil || standalone.Output != "�" || standalone.NextOffset != 2 {
		t.Fatalf("standalone continuation byte was rejected or consumed incorrectly: %+v %v", standalone, err)
	}
	if _, err := m.Detail(s, "binary-utf8", 4, 8); !errors.Is(err, ErrInvalid) {
		t.Fatalf("valid UTF-8 continuation offset accepted: %v", err)
	}
}
