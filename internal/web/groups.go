package web

import (
	"net/http"
	"os"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/peer"
)

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	all, err := (&peer.Bus{Root: s.Root}).ListGroups()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	owned, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	mine := map[string]bool{}
	for _, cfg := range owned {
		mine[cfg.Name] = true
	}
	var out []peer.Group
	for _, g := range all {
		ok := true
		for _, m := range g.Members {
			if !mine[m] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, g)
		}
	}
	if out == nil {
		out = []peer.Group{}
	}
	view, err := bot.Folders(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	folders := make([]folderJSON, 0, len(view.Folders))
	for _, f := range view.Folders {
		bots := f.Bots
		if bots == nil {
			bots = []string{}
		}
		folders = append(folders, folderJSON{Name: f.Name, Count: f.Count, Bots: bots, Ungrouped: f.Ungrouped})
	}
	order := view.Order
	if order == nil {
		order = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": order, "folders": folders, "groups": out})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Name    string    `json:"name"`
		Members *[]string `json:"members"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Members == nil {
		if err := bot.CreateFolder(s.Root, s.owner, in.Name); err != nil {
			writeFolderErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": strings.TrimSpace(in.Name)})
		return
	}
	g, err := (&peer.Bus{Root: s.Root, Now: time.Now}).CreateGroupForOwner(s.owner, in.Name, *in.Members)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) groupTranscript(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	bus := &peer.Bus{Root: s.Root}
	g, err := bus.LoadGroup(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.ownsGroup(g); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	lines, err := bus.Transcript(g.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if lines == nil {
		lines = []peer.Line{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": g, "messages": lines})
}

func (s *Server) postGroup(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	err := (&peer.Bus{Root: s.Root, Now: time.Now}).PostGroup(link.KindPerson, s.owner, r.PathValue("id"), in.Text, "", 1, time.Now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) ownsGroup(g peer.Group) error {
	owned, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return err
	}
	mine := map[string]bool{}
	for _, cfg := range owned {
		mine[cfg.Name] = true
	}
	for _, m := range g.Members {
		if !mine[m] {
			return os.ErrPermission
		}
	}
	return nil
}
