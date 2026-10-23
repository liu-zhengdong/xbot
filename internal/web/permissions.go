package web

import (
	"encoding/json"
	"io"
	"net/http"

	"xbot/internal/permissions"
)

// The Web and CLI use the same install-wide permission switch (ADR 0019).
func (s *Server) getPermissions(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	confirm := "off"
	if permissions.On(s.Root) {
		confirm = "table"
	}
	writeJSON(w, http.StatusOK, map[string]string{"confirm": confirm})
}

func (s *Server) putPermissions(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || (in.Confirm != "off" && in.Confirm != "table") {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeErr(w, http.StatusBadRequest, "expected one object")
		return
	}
	if err := permissions.Set(s.Root, in.Confirm == "table"); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.getPermissions(w, r)
}
