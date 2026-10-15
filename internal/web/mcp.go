package web

import (
	"errors"
	"net/http"
	"strings"

	"xbot/internal/link"
)

func (s *Server) listMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	list, err := link.LoadMCP(b.MCPPath())
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	if list == nil {
		list = []link.MCPServer{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": list})
}

func (s *Server) addMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in link.MCPServer
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := link.AddMCP(b.MCPPath(), in); err != nil {
		writeMCPErr(w, err)
		return
	}
	list, err := link.LoadMCP(b.MCPPath())
	if err != nil {
		writeMCPErr(w, err)
		return
	}
	want := strings.TrimSpace(in.Name)
	for _, e := range list {
		if e.Name == want {
			writeJSON(w, http.StatusOK, e)
			return
		}
	}
	writeErr(w, http.StatusBadRequest, "bad request")
}

func (s *Server) removeMCP(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	name := r.PathValue("server")
	if err := link.RemoveMCP(b.MCPPath(), name); err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func writeMCPErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, link.ErrMCPExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, link.ErrMCPMissing):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
