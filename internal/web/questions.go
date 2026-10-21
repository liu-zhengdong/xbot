package web

import (
	"errors"
	"net/http"
	"xbot/internal/kernel"
)

func (s *Server) chooseQuestion(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Choice string `json:"choice"`
	}
	if decode(r, &in) != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	err = kernel.ChooseQuestion(b, r.PathValue("id"), in.Choice)
	switch {
	case errors.Is(err, kernel.ErrNoQuestion):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, kernel.ErrQuestionAnswered):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, kernel.ErrQuestionChoice):
		writeErr(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
