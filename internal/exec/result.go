package exec

import (
	"fmt"
	"path/filepath"
	"strings"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
)

const resultPreviewBytes = 256

// ResultText stores the conclusion in the task's existing directory. Completion
// events carry a pointer, never a second full copy, even for short conclusions.
// Tool jobs keep their existing output policy through ResultTextAt.
func ResultText(dir, text, tail string) (string, error) {
	path := filepath.Join(dir, "result.txt")
	text = strings.TrimSpace(text)
	full := text
	if tail != "" {
		full += "\n\n" + tail
	}
	if err := fsutil.WriteAtomic(path, []byte(full), 0600); err != nil {
		return "", err
	}
	report := fmt.Sprintf("[结论全文：%s；用 read_file 分段读取]", path)
	if tail != "" {
		report += "\n\n" + tail
	}
	if len(report) >= reportBytes {
		return "", fmt.Errorf("result path exceeds report size limit")
	}
	// A short result is already a summary. Repeating it would still duplicate
	// the entire conclusion, so only longer results get a bounded opening.
	if len(text) > resultPreviewBytes && len(report)+2+resultPreviewBytes <= reportBytes {
		preview, _ := clip.Bytes(text, resultPreviewBytes)
		report = preview + "\n\n" + report
	}
	return report, nil
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
