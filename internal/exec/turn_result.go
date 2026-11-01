package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xbot/internal/clip"
)

// turnResult creates an immutable conclusion; a later turn cannot retarget it.
func turnResult(dir string, seq int, text, tail string) (string, string, error) {
	path := filepath.Join(dir, "results", strconv.Itoa(seq)+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	text = strings.TrimSpace(text)
	full := text
	if tail != "" {
		full += "\n\n" + tail
	}
	_, err = f.WriteString(full)
	closeErr := f.Close()
	if err != nil {
		return "", "", err
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	report := fmt.Sprintf("[结论全文：%s；用 read_file 分段读取]", path)
	if tail != "" {
		report += "\n\n" + tail
	}
	if len(report) >= reportBytes {
		return path, "", fmt.Errorf("result path exceeds report size limit")
	}
	if len(text) > resultPreviewBytes && len(report)+2+resultPreviewBytes <= reportBytes {
		preview, _ := clip.Bytes(text, resultPreviewBytes)
		report = preview + "\n\n" + report
	}
	return path, report, nil
}
