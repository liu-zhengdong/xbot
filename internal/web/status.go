package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/peer"
)

const statusStepsMax = 20

// statusSource is the kernel.StatusSource the web server attaches to its
// kernels: every same-owner bot in this process.
type statusSource struct{ s *Server }

// botStatuses lists all local bots. A bot with a live kernel here is derived;
// every other local bot is idle from its transcript tail. A local bot another
// process holds is reported running.
func (s *Server) botStatuses() []kernel.BotStatus {
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	kernels := make(map[string]*kernel.Kernel, len(s.kernels))
	for name, k := range s.kernels {
		kernels[name] = k
	}
	holds := make(map[string]bool, len(s.holds))
	for name := range s.holds {
		holds[name] = true
	}
	s.mu.Unlock()
	cursor, _ := s.readCursor()
	now := time.Now()
	out := make([]kernel.BotStatus, 0, len(cfgs))
	for _, cfg := range cfgs {
		if k := kernels[cfg.Name]; k != nil {
			st := k.Status()
			st.CompletedUnread = s.unreadCompletions(k.Bot, cursor[cfg.Name])
			out = append(out, st)
			continue
		}
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			continue
		}
		st := kernel.IdleStatus(b, now)
		st.CompletedUnread = s.unreadCompletions(b, cursor[cfg.Name])
		if holds[cfg.Name] {
			// Another process holds this bot. It may be running elsewhere.
			st.State = kernel.StateRunning
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bot < out[j].Bot })
	return out
}

// BotStatuses implements kernel.StatusSource.
func (src statusSource) BotStatuses() []kernel.BotStatus { return src.s.botStatuses() }

// BotSteps implements kernel.StatusSource.
func (src statusSource) BotSteps(name string, n int) []kernel.Step {
	return src.s.botSteps(name, n)
}

// botSteps returns up to n steps of a bot's current turn.
func (s *Server) botSteps(name string, n int) []kernel.Step {
	if n <= 0 || n > statusStepsMax {
		n = statusStepsMax
	}
	s.mu.Lock()
	k := s.kernels[name]
	s.mu.Unlock()
	if k != nil {
		return k.Steps(n)
	}
	return nil
}

// registerStatusAPI mounts the read-only status routes.
func (s *Server) registerStatusAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bots/status", s.botStatusAPI)
	mux.HandleFunc("GET /api/bots/{name}/steps", s.botStepsAPI)
	mux.HandleFunc("GET /api/bots/{name}/todos", s.botTodosAPI)
}

// botTodosAPI returns the bot's active list from the same file the tool and reminders use.
func (s *Server) botTodosAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	items, err := kernel.ReadTodos(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "todos unreadable")
		return
	}
	if items == nil {
		items = []kernel.TodoItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// botStatusAPI returns {bots:[BotStatus]} for every same-owner bot.
func (s *Server) botStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	bots := s.botStatuses()
	if bots == nil {
		bots = []kernel.BotStatus{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

// botStepsAPI returns {steps:[Step]} for one bot's current turn. n is capped.
func (s *Server) botStepsAPI(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	n := statusStepsMax
	if raw := strings.TrimSpace(r.URL.Query().Get("n")); raw != "" {
		got, err := strconv.Atoi(raw)
		if err != nil || got <= 0 {
			writeErr(w, http.StatusBadRequest, "bad n")
			return
		}
		if got < n {
			n = got
		}
	}
	steps := s.botSteps(b.Name(), n)
	if steps == nil {
		steps = []kernel.Step{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps})
}
