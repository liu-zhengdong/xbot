package web

import (
	"net/http"
	"path/filepath"

	"xbot/internal/skill"
)

type skillJSON struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	infos, err := skill.List(skill.OwnerDir(s.Root, s.owner))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	disabled := map[string]bool{}
	for _, slug := range b.Config().DisabledSkills {
		disabled[slug] = true
	}
	out := make([]skillJSON, 0, len(infos))
	for _, info := range infos {
		out = append(out, skillJSON{
			Slug: info.Slug, Name: info.Name, Description: info.Description,
			Disabled: disabled[info.Slug],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (s *Server) readSkill(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	if _, err := s.owned(r.PathValue("name")); err != nil {
		writeMissing(w, err)
		return
	}
	info, body, err := skill.Open(skill.OwnerDir(s.Root, s.owner), r.PathValue("slug"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug": info.Slug, "name": info.Name, "description": info.Description, "body": body,
	})
}

func (s *Server) disableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkill(w, r, true)
}

func (s *Server) enableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkill(w, r, false)
}

func (s *Server) setSkill(w http.ResponseWriter, r *http.Request, disable bool) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	slug := r.PathValue("slug")
	if _, _, err := skill.Open(filepath.Join(skill.Dir(s.Root), s.owner), slug); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if disable {
		err = b.DisableSkill(slug)
	} else {
		err = b.EnableSkill(slug)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": slug, "disabled": disable})
}
