package bot

import (
	"encoding/json"
	"strings"
)

// Environment publications and file-write receipts share one framing. The
// transcript keeps a complete receipt beside a blob preview; the payload's
// meaning belongs to the kernel, not to the storage layer.
const EnvironmentOpen = "<environment_update>\n"
const EnvironmentClose = "\n</environment_update>"

// SplitEnvironmentReceipt separates the last complete JSON receipt from a tool
// result. Reminders may follow it, and remain on the same original result.
func SplitEnvironmentReceipt(text string) (body, receipt, after string) {
	i := strings.LastIndex(text, "\n\n"+EnvironmentOpen)
	if i < 0 {
		return text, "", ""
	}
	start := i + 2
	j := strings.Index(text[start+len(EnvironmentOpen):], EnvironmentClose)
	if j < 0 {
		return text, "", ""
	}
	end := start + len(EnvironmentOpen) + j
	if !json.Valid([]byte(text[start+len(EnvironmentOpen) : end])) {
		return text, "", ""
	}
	end += len(EnvironmentClose)
	return text[:i], text[start:end], text[end:]
}
