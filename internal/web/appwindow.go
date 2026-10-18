package web

import (
	"net/http"
	"strings"
)

// displayVersion is the build version as the window's title bar shows it:
// no leading "v", and nothing after "+" (a build or commit suffix).
func displayVersion(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimPrefix(v, "v")
}

// appTheme lets the page make the native window buttons follow XBot's own
// theme. Only the desktop app has such buttons; a plain serve has none.
func (s *Server) appTheme(w http.ResponseWriter, r *http.Request) {
	if !s.App {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if !s.require(w, r) {
		return
	}
	var in struct {
		Theme string `json:"theme"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	switch in.Theme {
	case "light", "dark", "system":
	default:
		writeErr(w, http.StatusBadRequest, "bad theme")
		return
	}
	if s.AppTheme != nil {
		if err := s.AppTheme(in.Theme); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
