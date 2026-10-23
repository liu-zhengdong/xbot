package exec

import (
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
)

// ResultText uses the task's existing directory. The report remains an ordinary
// event; read_file can read the complete text in bounded ranges when necessary.
// tail reserves the same bounded space for a kernel-authored notable digest.
func ResultText(dir, text, tail string) (string, error) {
	return ResultTextAt(filepath.Join(dir, "result.txt"), text, tail)
}

// ResultTextAt shares the same bounded preview with an existing tool-job output.
func ResultTextAt(path, text, tail string) (string, error) {
	text = strings.TrimSpace(text)
	if len(text) <= reportBytes {
		return text, nil
	}
	if err := fsutil.WriteAtomic(path, []byte(text), 0600); err != nil {
		return "", err
	}
	return PreviewResult(path, text, tail)
}

// PreviewResult refers to a complete result already stored by its owner.
func PreviewResult(path, text, tail string) (string, error) {
	if len(text) <= reportBytes {
		return text, nil
	}
	suffix := fmt.Sprintf("\n\n[结果较长，全文：%s；需要完整结论和关键数字时用read_file分段读取]", path)
	if tail != "" {
		suffix += "\n\n" + tail
	}
	if len(suffix) >= reportBytes {
		return "", fmt.Errorf("result path exceeds report size limit")
	}
	preview, _ := clip.Bytes(text, reportBytes-len(suffix))
	return preview + suffix, nil
}

func (j *job) isBlocked() bool { j.mu.Lock(); defer j.mu.Unlock(); return j.blocked }
