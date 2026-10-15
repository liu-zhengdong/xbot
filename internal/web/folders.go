package web

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"xbot/internal/bot"
)

type folderJSON struct {
	Name      string   `json:"name"`
	Count     int      `json:"count"`
	Bots      []string `json:"bots"`
	Ungrouped bool     `json:"ungrouped,omitempty"`
}

func (s *Server) patchFolder(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := bot.RenameFolder(s.Root, s.owner, r.PathValue("id"), in.Name); err != nil {
		writeFolderErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": strings.TrimSpace(in.Name)})
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	name := r.PathValue("id")
	if err := bot.DeleteFolder(s.Root, s.owner, name); err != nil {
		writeFolderErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": strings.TrimSpace(name)})
}

func writeFolderErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bot.ErrFolderExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, bot.ErrFolderMissing), errors.Is(err, bot.ErrNotFound), errors.Is(err, os.ErrPermission):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
