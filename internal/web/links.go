package web

import (
	"errors"
	"net/http"

	"xbot/internal/bot"
	"xbot/internal/link"
)

type linkJSON struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Kind         string   `json:"kind"`
	Capabilities []string `json:"capabilities"`
	Secrets      []string `json:"secrets"`
	Summary      string   `json:"summary"`
	Available    bool     `json:"available"`
	Installed    *bool    `json:"installed,omitempty"`
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": catalogRows(nil)})
}

func (s *Server) listBotLinks(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": catalogRows(b)})
}

func (s *Server) installLink(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	name, err := link.GrantName(in.ID)
	if err != nil {
		writeLinkErr(w, err)
		return
	}
	if err := b.GrantLink(name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	on := true
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "installed": on})
}

func (s *Server) removeLink(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	id := r.PathValue("id")
	e, ok := link.Lookup(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if e.Grant != "" {
		if err := b.RevokeLink(e.Grant); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "installed": false})
}

func catalogRows(b *bot.Bot) []linkJSON {
	granted := map[string]bool{}
	if b != nil {
		for _, name := range b.Config().Links {
			granted[name] = true
		}
	}
	rows := make([]linkJSON, 0, len(link.Catalog()))
	for _, e := range link.Catalog() {
		caps := e.Capabilities
		if caps == nil {
			caps = []string{}
		}
		secrets := e.Secrets
		if secrets == nil {
			secrets = []string{}
		}
		row := linkJSON{
			ID: e.ID, Title: e.Title, Kind: e.Kind, Capabilities: caps, Secrets: secrets,
			Summary: e.Summary, Available: e.Available,
		}
		if b != nil {
			on := e.Grant != "" && granted[e.Grant]
			row.Installed = &on
		}
		rows = append(rows, row)
	}
	return rows
}

func writeLinkErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, link.ErrUnknown):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, link.ErrUnavailable):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
