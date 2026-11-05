package web

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const resourceTextLimit = 64 << 10

// resourcePath validates a target once at each HTTP entry point.
func resourcePath(path string) (string, error) {
	if path == "" || len(path) > 4096 || strings.ContainsRune(path, 0) {
		return "", errors.New("路径为空、含 NUL 或超过 4096 字节")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("无法确定当前用户的主目录")
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) || len(path) > 4096 {
		return "", errors.New("请提供当前设备的绝对路径或 ~/ 路径")
	}
	return filepath.Clean(path), nil
}

func resourceReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		writeErr(w, http.StatusNotFound, "文件或目录不存在")
	case errors.Is(err, os.ErrPermission):
		writeErr(w, http.StatusForbidden, "没有权限读取此文件或目录")
	default:
		writeErr(w, http.StatusInternalServerError, "读取文件或目录失败："+err.Error())
	}
}

func (s *Server) resource(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	query := r.URL.Query()
	path, err := resourcePath(query.Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, line := 0, 1
	if raw := query.Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			writeErr(w, http.StatusBadRequest, "目录偏移必须是非负整数")
			return
		}
	}
	if raw := query.Get("line"); raw != "" {
		line, err = strconv.Atoi(raw)
		if err != nil || line < 1 {
			writeErr(w, http.StatusBadRequest, "行号必须是正整数")
			return
		}
	}
	if r.Context().Err() != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		resourceReadError(w, err)
		return
	}
	device, err := os.Hostname()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "无法读取当前设备名称")
		return
	}
	kind := "unsupported"
	if info.IsDir() {
		kind = "directory"
	} else if info.Mode().IsRegular() {
		kind = "file"
	}
	out := map[string]any{
		"path": path, "name": filepath.Base(path), "kind": kind,
		"device": device, "platform": runtime.GOOS,
		"can_open": kind != "unsupported" && s.App && s.OpenPath != nil,
	}
	if kind == "unsupported" {
		out["preview_error"] = "此文件类型不支持预览"
		writeJSON(w, http.StatusOK, out)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		resourceReadError(w, err)
		return
	}
	defer f.Close()
	if kind == "directory" {
		type entry struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}
		entries := make([]entry, 0, 40)
		for index := 0; ; index++ {
			if r.Context().Err() != nil {
				return
			}
			items, err := f.ReadDir(1)
			if err != nil && !errors.Is(err, io.EOF) {
				resourceReadError(w, err)
				return
			}
			if len(items) == 0 {
				break
			}
			if index < offset {
				continue
			}
			if len(entries) == 40 {
				out["next_offset"] = index
				break
			}
			item := items[0]
			itemKind := "unsupported"
			switch {
			case item.Type()&os.ModeSymlink != 0:
				itemKind = "link"
			case item.IsDir():
				itemKind = "directory"
			case item.Type().IsRegular():
				itemKind = "file"
			}
			entries = append(entries, entry{Name: item.Name(), Kind: itemKind})
		}
		out["entries"] = entries
	} else {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), resourceTextLimit+1)
		// Keep line endings so the page budget measures original bytes.
		scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			i := bytes.IndexByte(data, '\n')
			chunk := data
			if i >= 0 {
				chunk = data[:i+1]
			}
			for _, b := range chunk {
				if b < 32 && b != '\n' && b != '\r' && b != '\t' || b == 127 {
					return 0, nil, errors.New("二进制文件不支持文本预览")
				}
			}
			if i >= 0 {
				return i + 1, chunk, nil
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		var text strings.Builder
		previewError := ""
		for current := 1; ; current++ {
			if r.Context().Err() != nil {
				return
			}
			if !scanner.Scan() {
				break
			}
			chunk := scanner.Bytes()
			if len(chunk) > resourceTextLimit {
				previewError = "单行超过 64 KiB，无法预览"
				break
			}
			if !utf8.Valid(chunk) {
				previewError = "文件不是有效的 UTF-8 文本，无法预览"
				break
			}
			if current < line {
				continue
			}
			if text.Len()+len(chunk) > resourceTextLimit {
				out["next_line"] = current
				break
			}
			text.Write(chunk)
		}
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				previewError = "单行超过 64 KiB，无法预览"
			} else {
				previewError = "无法预览：" + err.Error()
			}
		}
		if previewError != "" {
			delete(out, "next_line")
			out["preview_error"] = previewError
		} else {
			out["text"], out["start_line"] = text.String(), line
		}
	}
	if r.Context().Err() != nil {
		return
	}
	// A 64 KiB page remains below 1 MiB even after JSON escaping.
	// Directory pages contain at most 40 names, without parent paths.
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) openResource(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := decode(r, &input); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	path, err := resourcePath(input.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.App || s.OpenPath == nil {
		writeErr(w, http.StatusNotFound, "当前实例不支持在系统中打开")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		resourceReadError(w, err)
		return
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		writeErr(w, http.StatusBadRequest, "只能打开普通文件或目录")
		return
	}
	if err := s.OpenPath(path); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
