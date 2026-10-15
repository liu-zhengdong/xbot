package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
)

func paintBot(b *bot.Bot) (url, bg, ink, initial string) {
	bg, ink, initial = bot.AvatarColors(b.Name())
	if b.Config().Avatar == bot.AvatarCustom || bot.HasAvatarGenerator() {
		url = "/api/bots/" + b.Name() + "/avatar"
	}
	return url, bg, ink, initial
}

func (s *Server) forgetKernel(name string) {
	s.mu.Lock()
	delete(s.kernels, name)
	delete(s.gate, name)
	s.mu.Unlock()
}

type profileBody struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Tone        *string `json:"tone"`
	Length      *string `json:"length"`
	Emoji       *string `json:"emoji"`
	Language    *string `json:"language"`
	// Proactivity is accepted from older clients and ignored.
	Proactivity *string `json:"proactivity"`
	QuietStart  *string `json:"quiet_start"`
	QuietEnd    *string `json:"quiet_end"`
	Timezone    *string `json:"timezone"`
	Persona     *string `json:"persona"`
	Group       *string `json:"group"`
}

func (s *Server) patchBot(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in profileBody
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Persona != nil {
		if err := b.WritePersona(*in.Persona); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	u := bot.ProfileUpdate{
		Description: in.Description, Tone: in.Tone, Length: in.Length, Emoji: in.Emoji,
		Language: in.Language, QuietStart: in.QuietStart, QuietEnd: in.QuietEnd,
		Timezone: in.Timezone,
	}
	if u != (bot.ProfileUpdate{}) {
		if err := b.ApplyProfile(u); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	name := b.Name()
	if in.Name != nil {
		next := strings.TrimSpace(*in.Name)
		if next != "" && next != b.Name() {
			if err := kernel.RenameBot(s.Root, b.Name(), next); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			s.forgetKernel(b.Name())
			name = next
		}
	}
	if in.Group != nil {
		if err := bot.SetBotFolder(s.Root, s.owner, name, *in.Group); err != nil {
			writeFolderErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *Server) getAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	data, mime, ok, err := b.AvatarBytes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(data)
}

func (s *Server) postAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "bad avatar")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad avatar")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad avatar")
		return
	}
	mime := ""
	if hdr != nil {
		mime = hdr.Header.Get("Content-Type")
	}
	if err := b.SetAvatar(data, mime); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": b.Name(), "avatar": "custom"})
}

func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	if err := b.ClearAvatar(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": b.Name(), "avatar": "color"})
}

func (s *Server) exportTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	tpl, err := bot.ExportTemplate(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+b.Name()+".json\"")
	writeJSON(w, http.StatusOK, tpl)
}

func (s *Server) importTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var tpl bot.Template
	if err := decodeLimit(r, &tpl, 2<<20); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	links := s.DefaultLinks
	if len(links) == 0 {
		links = []string{link.NameTerminal, link.NameTools, link.NameTimer, link.NameMemory, link.NameWeb, link.NamePeers}
	}
	existing, err := bot.List(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	b, err := bot.ImportTemplate(s.Root, tpl, links, s.owner)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := b.WriteRules(kernel.DefaultRules()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.adoptCreated(b.Name(), existing, links); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": b.Name()})
}

func (s *Server) adoptCreated(name string, existing []bot.Config, links []string) error {
	if len(existing) == 0 {
		return bot.SetPrimary(s.Root, name)
	}
	if _, err := bot.Primary(s.Root); errors.Is(err, bot.ErrNoPrimary) || errors.Is(err, bot.ErrNotFound) {
		_, _, err = bot.EnsurePrimary(s.Root, bot.Seed{Links: links, Owner: s.owner})
		return err
	}
	return nil
}

func decodeLimit(r *http.Request, v any, n int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, n))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
