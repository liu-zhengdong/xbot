package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/tools"
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
		Enabled  *bool   `json:"enabled"`
		Precheck *string `json:"precheck"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&body); err != nil || (body.Enabled == nil && body.Precheck == nil) {
		writeErr(w, 400, "expected enabled boolean or precheck string")
		return
	}
	if body.Precheck != nil && len(*body.Precheck) > tools.MaxPrecheckBytes {
		writeErr(w, 400, "precheck exceeds 16 KiB")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeErr(w, 400, "expected one object")
		return
	}
	var updated link.Routine
	var reviewErr error
	err = (&link.Timer{Dir: b.RoutineDir()}).Update(r.PathValue("id"), func(rt *link.Routine) error {
		if body.Enabled != nil {
			rt.Enabled = *body.Enabled
		}
		if body.Precheck != nil {
			rt.Precheck = *body.Precheck
		}
		reviewErr = kernel.CheckRoutinePrecheck(r.Context(), b, rt.Precheck)
		if reviewErr != nil {
			return reviewErr
		}
		updated = *rt
		return nil
	})
	if err != nil {
		if reviewErr != nil {
			writeErr(w, http.StatusForbidden, reviewErr.Error())
		} else if errors.Is(err, os.ErrNotExist) {
			writeErr(w, 404, "routine not found")
		} else {
			writeErr(w, 500, err.Error())
		}
		return
	}
	writeJSON(w, 200, updated)
}
