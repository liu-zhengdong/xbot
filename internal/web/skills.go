package web

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"

	"xbot/internal/bot"
	"xbot/internal/peer"
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
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	if b, err = s.owned(r.PathValue("name")); err != nil {
		writeMissing(w, err)
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

// The settings page works on the owner's whole library; only the on/off switches are per bot.

func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	infos, err := skill.List(skill.OwnerDir(s.Root, s.owner))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取技能")
		return
	}
	out := make([]map[string]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, map[string]string{"slug": info.Slug, "name": info.Name, "description": info.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// readLibrarySkill returns the skill's name and description plus, for each of the owner's bots, whether it is on.
// The body stays out: the settings page shows it read-only and short.
func (s *Server) readLibrarySkill(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	info, _, err := skill.Open(skill.OwnerDir(s.Root, s.owner), r.PathValue("slug"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "没有找到这个技能")
		return
	}
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取 Bot")
		return
	}
	bots := []map[string]any{}
	for _, cfg := range bot.Order(cfgs) {
		// A bot with several owners never loads a personal library (the kernel needs a sole owner),
		// so its switch would save and change nothing; say so instead of offering it.
		if len(cfg.Owners) != 1 {
			bots = append(bots, map[string]any{"name": cfg.Name, "enabled": false, "shared": true})
			continue
		}
		bots = append(bots, map[string]any{"name": cfg.Name, "enabled": !slices.Contains(cfg.DisabledSkills, info.Slug)})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug": info.Slug, "name": info.Name, "description": info.Description, "bots": bots,
	})
}

// importSkill takes the SKILL.md the browser read from a folder the owner picked.
// Nothing here names a path on this machine: the folder name only proposes the skill's directory name,
// and ImportData accepts it only when it is a plain slug.
func (s *Server) importSkill(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Folder    string `json:"folder"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "没能读取这个文件夹，请换一个再试")
		return
	}
	res, err := skill.ImportData(s.Root, s.owner, in.Folder, []byte(in.Content), in.Overwrite)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"slug": res.Slug, "name": res.Name})
	case errors.Is(err, skill.ErrExists):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "库里已有同名技能，要覆盖吗？", "conflict": true, "name": res.Name})
	case errors.Is(err, skill.ErrNoMeta):
		writeErr(w, http.StatusBadRequest, "这个文件夹里没找到技能说明，请选择一个技能文件夹")
	case errors.Is(err, skill.ErrBadSlug):
		writeErr(w, http.StatusBadRequest, "这个文件夹的名字不能用，请改成小写英文、数字或短横线后再导入")
	default:
		writeErr(w, http.StatusInternalServerError, "没能导入，请再试一次")
	}
}
