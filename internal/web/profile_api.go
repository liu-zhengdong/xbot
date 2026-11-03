package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
)

func paintBot(b *bot.Bot) (url, bg, ink, initial string) {
	bg, ink, initial = bot.AvatarColors(b.Name())
	if b.Config().Avatar == bot.AvatarCustom {
		url = "/api/bots/" + b.Name() + "/avatar"
	}
	return url, bg, ink, initial
}

func (s *Server) forgetKernel(name string) {
	s.mu.Lock()
	k := s.cachedKernelLocked(name)
	if entry := s.kernels[name]; entry != nil {
		entry.Kernel = nil
	}
	s.mu.Unlock()
	if k != nil {
		k.CloseOperations()
	}
}

// CloseOperations releases cached kernels' local listeners when the host
// service ends. It does not change the existing Jobs shutdown policy.
func (s *Server) CloseOperations() {
	s.mu.Lock()
	list := make([]*kernel.Kernel, 0, len(s.kernels))
	for _, entry := range s.kernels {
		if entry.Kernel != nil {
			list = append(list, entry.Kernel)
		}
	}
	s.mu.Unlock()
	for _, k := range list {
		k.CloseOperations()
	}
}

type profileBody struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	// Proactivity is accepted from older clients and ignored.
	Proactivity *string `json:"proactivity"`
	Timezone    *string `json:"timezone"`
	Persona     *string `json:"persona"`
	Group       *string `json:"group"`
	AvatarSeed  *string `json:"avatar_seed"`
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
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: in.Name, Description: in.Description, Timezone: in.Timezone, AvatarSeed: in.AvatarSeed})
	if err != nil {
		writeErr(w, http.StatusBadRequest, bot.RedactText(err.Error()))
		return
	}
	// Persona and folders remain separate writes, but their predictable input
	// failures must not occur after a successful profile change.
	if in.Persona != nil {
		persona := strings.TrimSpace(*in.Persona)
		if persona == "" || len(persona) > 32<<10 {
			writeErr(w, http.StatusBadRequest, "bot: invalid persona")
			return
		}
		in.Persona = &persona
	}
	if in.Group != nil {
		group := strings.TrimSpace(*in.Group)
		order, err := bot.LoadOrder(s.Root, s.owner)
		if err != nil {
			writeFolderErr(w, err)
			return
		}
		found := group == ""
		for _, name := range order {
			found = found || name == group
		}
		if group == bot.UngroupedName {
			writeErr(w, http.StatusBadRequest, "bot: reserved group name")
			return
		}
		if !found {
			writeFolderErr(w, bot.ErrFolderMissing)
			return
		}
		in.Group = &group
	}
	if err := bot.CheckProfileChange(s.Root, b.Name(), change); err != nil {
		writeErr(w, http.StatusBadRequest, bot.RedactText(err.Error()))
		return
	}
	resources, err := s.ResourcesFor(b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, bot.RedactText(err.Error()))
		return
	}
	value, err := s.applyProfileRuntime(b.Name(), change)
	if err != nil {
		writeProfileResult(w, value, err)
		return
	}
	if in.Persona != nil || in.Group != nil {
		save := func(current *bot.Bot, _ *inbox.Inbox) error {
			value.Name = current.Name()
			if in.Persona != nil {
				if err := current.WritePersona(*in.Persona); err != nil {
					return err
				}
				value.AppliedFields = append(value.AppliedFields, "persona")
				value.Applied = true
			}
			if in.Group != nil {
				if err := bot.SetBotFolder(s.Root, s.owner, current.Name(), *in.Group); err != nil {
					return err
				}
				value.AppliedFields = append(value.AppliedFields, "group")
				value.Applied = true
			}
			return nil
		}
		if resources != nil {
			err = resources.WithCurrentIO(save)
		} else {
			var current *bot.Bot
			current, err = bot.Load(s.Root, value.Name)
			if err == nil {
				err = save(current, nil)
			}
		}
	}
	writeProfileResult(w, value, err)
}

// A domain error can follow a real save or move. Keep the full receipt so a
// client can adopt the actual name before displaying the failure.
func writeProfileResult(w http.ResponseWriter, value cliops.Profile, err error) {
	status := http.StatusOK
	if err != nil {
		status = http.StatusBadRequest
		if value.Error == "" {
			value.Error = bot.RedactText(err.Error())
		}
	} else if value.Error != "" {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, value)
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
	// Only an upload is served here. Generated avatars are drawn in the browser.
	if b.Config().Avatar != bot.AvatarCustom {
		http.NotFound(w, r)
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
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
	if len(data) > 1<<20 {
		writeErr(w, http.StatusBadRequest, "bot: avatar exceeds 1 MiB")
		return
	}
	value, err := s.applyAvatarRuntime(b.Name(), data, false)
	writeProfileResult(w, value, err)
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
	value, err := s.applyAvatarRuntime(b.Name(), nil, true)
	writeProfileResult(w, value, err)
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
