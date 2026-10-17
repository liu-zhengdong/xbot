package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"xbot/internal/clip"
)

const (
	defaultReadLines = 200
	maxReadLines     = 2000
	maxReadBytes     = 32 << 10
	maxLineBytes     = 32 << 10
)

// ReadFile reads a text file by line offset.
// Root is the base for relative paths. Empty uses the working directory.
// Sandbox, when set, rejects any path that resolves outside it.
type ReadFile struct {
	MaxLines int
	MaxBytes int
	Root     string
	Sandbox  string
	rootBind
}

func (r *ReadFile) Name() string { return "read_file" }

func (r *ReadFile) Description() string {
	return "Read a text file. offset is the 1-based starting line. limit is how many lines to return. force reads it again even when the file is unchanged since the last read."
}

func (r *ReadFile) Parameters() any {
	return objectSchema(map[string]any{
		"path":   stringProp("File path"),
		"offset": intProp("1-based line number to start at (default 1)"),
		"limit":  intProp("Maximum number of lines (default 200)"),
		"force":  boolProp("Return the file again even if it is unchanged since the last read"),
	}, []string{"path"})
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (r *ReadFile) Confirm(args json.RawMessage) (bool, string, error) {
	in, path, err := r.parse(args)
	if err != nil {
		return false, "", err
	}
	return false, fmt.Sprintf("read %s offset %d limit %d", DisplayPath(r.Sandbox, path), normOffset(in.Offset), r.normLimit(in.Limit)), nil
}

// ResolvedPath reports the absolute path this call would read, so the kernel
// can decide whether it reaches a protected vault path.
func (r *ReadFile) ResolvedPath(args json.RawMessage) string {
	_, path, err := r.parse(args)
	if err != nil {
		return ""
	}
	return path
}

// ShownPath is the path the model should see. A sandbox rewrites the host path.
func (r *ReadFile) ShownPath(abs string) string { return DisplayPath(r.Sandbox, abs) }

func (r *ReadFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	in, path, err := r.parse(args)
	if err != nil {
		return "", err
	}
	label := DisplayPath(r.Sandbox, path)
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %s", RewriteHost(r.Sandbox, path, err.Error()))
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read_file: %s", RewriteHost(r.Sandbox, path, err.Error()))
	}
	if st.IsDir() {
		return "", fmt.Errorf("read_file: %s is a directory", label)
	}
	offset := normOffset(in.Offset)
	limit := r.normLimit(in.Limit)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var b strings.Builder
	lineNo := 0
	shown := 0
	first := 0
	last := 0
	bytes := 0
	truncated := false
	maxBytes := r.maxBytes()
	for sc.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if shown >= limit {
			break
		}
		line := sc.Text()
		piece := fmt.Sprintf("%d|%s\n", lineNo, line)
		if bytes+len(piece) > maxBytes {
			truncated = true
			break
		}
		if shown == 0 {
			first = lineNo
		}
		b.WriteString(piece)
		bytes += len(piece)
		shown++
		last = lineNo
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	if shown == 0 {
		return fmt.Sprintf("%s: offset %d is past end of file (%d lines)", DisplayPath(r.Sandbox, path), offset, lineNo), nil
	}
	body := clip.Valid(strings.TrimRight(b.String(), "\n"))
	head := fmt.Sprintf("%s lines %d-%d", DisplayPath(r.Sandbox, path), first, last)
	if truncated {
		return head + "\n" + body + "\n[truncated]", nil
	}
	return head + "\n" + body, nil
}

func (r *ReadFile) parse(args json.RawMessage) (readArgs, string, error) {
	var in readArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return readArgs{}, "", fmt.Errorf("read_file: %w", err)
	}
	path, err := ResolveRoot(r.rootBind.dir(r.Root), AliasCase(r.Sandbox, in.Path))
	if err != nil {
		return readArgs{}, "", fmt.Errorf("read_file: %w", err)
	}
	if err := Confine(r.Sandbox, path); err != nil {
		return readArgs{}, "", fmt.Errorf("read_file: %w", err)
	}
	return in, path, nil
}

func normOffset(offset int) int {
	if offset < 1 {
		return 1
	}
	return offset
}

func (r *ReadFile) normLimit(limit int) int {
	if limit <= 0 {
		limit = defaultReadLines
	}
	max := r.MaxLines
	if max <= 0 {
		max = maxReadLines
	}
	if limit > max {
		return max
	}
	return limit
}

func (r *ReadFile) maxBytes() int {
	if r.MaxBytes <= 0 {
		return maxReadBytes
	}
	return r.MaxBytes
}
