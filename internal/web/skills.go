package web

import (
	"errors"
	"io"
	"net/http"
	"os"
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
		Source    string `json:"source"`
		ID        string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "没能读取这个文件夹，请换一个再试")
		return
	}
	folder, raw := in.Folder, []byte(in.Content)
	if in.Source != "" {
		var ok bool
		if folder, raw, ok = s.readSourceSkill(w, in.Source, in.ID); !ok {
			return
		}
	}
	res, err := skill.ImportData(s.Root, s.owner, folder, raw, in.Overwrite)
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

// Where the owner's other tools keep skills, under the home directory. The page only ever sees the ids.
var skillSources = map[string]string{
	"agent":  filepath.Join(".agents", "skills"),
	"codex":  filepath.Join(".codex", "skills"),
	"claude": filepath.Join(".claude", "skills"),
}

// maxSourceSkill matches the cap on a SKILL.md the folder picker sends.
const maxSourceSkill = 256 << 10

func (s *Server) sourceRoot(source string) (string, bool) {
	rel, ok := skillSources[source]
	if !ok {
		return "", false
	}
	home := s.SkillHome
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", false
		}
	}
	return filepath.Join(home, rel), true
}

// listSourceSkills lists the skills one source holds: its first-level skill folders only, read-only.
// A missing library is simply empty.
func (s *Server) listSourceSkills(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	root, ok := s.sourceRoot(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "没有这个来源")
		return
	}
	infos, err := skill.List(root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "没能读取技能")
		return
	}
	out := make([]map[string]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, map[string]string{"id": info.Slug, "name": info.Name, "description": info.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": r.PathValue("id"), "skills": out})
}

// readSourceSkill loads one skill from a source library. id must be a plain skill folder name,
// so nothing outside that source's first level can be named. On failure it has already answered.
func (s *Server) readSourceSkill(w http.ResponseWriter, source, id string) (string, []byte, bool) {
	root, ok := s.sourceRoot(source)
	if !ok {
		writeErr(w, http.StatusNotFound, "没有这个来源")
		return "", nil, false
	}
	if !skill.ValidSlug(id) {
		writeErr(w, http.StatusBadRequest, "这条技能不能导入")
		return "", nil, false
	}
	f, err := os.Open(filepath.Join(root, id, "SKILL.md"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "没有找到这个技能")
		return "", nil, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSourceSkill+1))
	if err != nil || len(raw) > maxSourceSkill {
		writeErr(w, http.StatusBadRequest, "技能说明太大了，导入不了")
		return "", nil, false
	}
	return id, raw, true
}
