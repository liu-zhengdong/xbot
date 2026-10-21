package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"xbot/internal/link"
)

func (s *Server) patchRoutine(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&body); err != nil || body.Enabled == nil {
		writeErr(w, 400, "enabled must be a boolean")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeErr(w, 400, "expected one object")
		return
	}
	var updated link.Routine
	err = (&link.Timer{Dir: b.RoutineDir()}).Update(r.PathValue("id"), func(rt *link.Routine) error { rt.Enabled = *body.Enabled; updated = *rt; return nil })
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, 404, "routine not found")
		} else {
			writeErr(w, 500, err.Error())
		}
		return
	}
	writeJSON(w, 200, updated)
}
