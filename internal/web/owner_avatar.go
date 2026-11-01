package web

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

func (s *Server) ownerAvatarURL() string {
	if _, err := os.Stat(filepath.Join(s.Root, "owner-avatar")); err == nil {
		return "/api/owner/avatar"
	}
	return ""
}

func (s *Server) ownerAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	path := filepath.Join(s.Root, "owner-avatar")
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", bot.SniffImage(data))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		_, _ = w.Write(data)
		return
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			writeErr(w, 400, "bad avatar")
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, 400, "bad avatar")
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		if err != nil {
			writeErr(w, 400, "bad avatar")
			return
		}
		if len(data) > 1<<20 {
			writeErr(w, 413, "avatar is too large")
			return
		}
		if bot.SniffImage(data) == "" {
			writeErr(w, 400, "unsupported avatar type")
			return
		}
		if err := fsutil.WriteAtomic(path, data, 0600); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	case http.MethodDelete:
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			writeErr(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]string{"owner_avatar_url": s.ownerAvatarURL()})
}
