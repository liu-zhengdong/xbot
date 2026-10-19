package web

import (
	"net/http"

	"xbot/internal/kernel"
)

// registerContextAPI mounts the read-only context inspector route. It reports
// what the model was actually sent on the current wake: split by band, with a
// clipped copy of every message. Nothing is written.
func (s *Server) registerContextAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bots/{name}/context", s.botContextAPI)
}

// botContextAPI returns the requests of the bot's current wake.
//
// A bot with no live kernel here has no requests to show: the snapshot lives in
// memory for one wake, and a bot driven by another process (or before the first
// wake) has none. It answers with an empty list and a reason rather than 404,
// because "no request yet" is a normal state, not a missing bot.
func (s *Server) botContextAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	s.mu.Lock()
	k := s.kernels[b.Name()]
	s.mu.Unlock()
	snap := kernel.ContextSnap{Bot: b.Name()}
	if k != nil {
		snap = k.ContextSnapshot()
	}
	if snap.Requests == nil {
		snap.Requests = []kernel.ContextRequest{}
	}
	writeJSON(w, http.StatusOK, snap)
}
