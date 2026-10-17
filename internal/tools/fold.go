package tools

// indexFold is strings.Index with ASCII case folding.
// The returned offset is always an index into s. strings.ToLower is not used:
// some letters grow when lowercased (U+0130 "İ" is 2 bytes and becomes 3),
// and an offset into that copy panics when used on s.
func indexFold(s, needle string) int {
	if needle == "" {
		return 0
	}
	if len(needle) > len(s) {
		return -1
	}
	n0 := foldASCII(needle[0])
	last := len(s) - len(needle)
	for i := 0; i <= last; i++ {
		if foldASCII(s[i]) != n0 {
			continue
		}
		if equalFoldASCII(s[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func foldASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func equalFoldASCII(s, needle string) bool {
	for i := 0; i < len(needle); i++ {
		if foldASCII(s[i]) != foldASCII(needle[i]) {
			return false
		}
	}
	return true
}
