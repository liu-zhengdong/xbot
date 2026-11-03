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
// A bot with no live kernel here answers from the snapshot the last wake left
// on disk, marked stale, so a restart does not empty the panel. A bot that was
// never woken still has none and answers with an empty list rather than 404,
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
	k := s.cachedKernelLocked(b.Name())
	s.mu.Unlock()
	snap := kernel.ContextSnap{Bot: b.Name()}
	if k != nil {
		snap = k.ContextSnapshot()
	}
	if len(snap.Requests) == 0 {
		if saved, ok := kernel.LoadContextSnap(b); ok {
			snap = saved
			snap.Bot = b.Name()
			snap.Stale = true
		}
	}
	if snap.Requests == nil {
		snap.Requests = []kernel.ContextRequest{}
	}
	writeJSON(w, http.StatusOK, snap)
}
