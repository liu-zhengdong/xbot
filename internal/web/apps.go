package web

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/peer"
)

// The app store installs for the owner, not for a bot: nothing under /api/apps
// names one. Installed apps are written to the owner-level files under the
// root (apps.json, mcp.json), which every bot reads.

// keyPerBotNote is shown on key-bearing apps, whose key is saved per Bot rather than shared.
const keyPerBotNote = "密钥按 Bot 分别保存，只有填过密钥的 Bot 能用。"

type appJSON struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// Scope is a limit on who can use the app, shown apart from the summary so it is never cut off.
	Scope    string `json:"scope,omitempty"`
	Category string `json:"category"`
	// State is available, connected, or soon.
	State string `json:"state"`
	// Card means connecting asks for a key on a confirmation card in chat.
	Card   bool `json:"card,omitempty"`
	Custom bool `json:"custom,omitempty"`
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	rows, err := s.appRows()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "apps unavailable")
		return
	}
	n := 0
	for _, a := range rows {
		if a.State == "connected" {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": n, "apps": rows})
}

func (s *Server) appRows() ([]appJSON, error) {
	apps, err := link.StoreApps()
	if err != nil {
		return nil, err
	}
	global, err := bot.GlobalLinks(s.Root)
	if err != nil {
		return nil, err
	}
	servers, err := link.LoadMCP(bot.GlobalMCPPath(s.Root))
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, n := range global {
		have[n] = true
	}
	// A key-bearing connector keeps its key in the vault of the bot that took it,
	// so it counts as connected when any of the owner's bots holds the grant.
	cfgs, _ := peer.OwnerBots(s.Root, s.owner)
	for _, cfg := range cfgs {
		for _, n := range cfg.Links {
			if n == link.NameTelegram || n == link.NameLark {
				have[n] = true
			}
		}
	}
	keyHeld := s.ownerKeyHeld(cfgs)
	srv := map[string]link.MCPServer{}
	for _, v := range servers {
		srv[v.Name] = v
	}
	rows := make([]appJSON, 0, len(apps)+len(servers))
	known := map[string]bool{}
	for _, a := range apps {
		known[a.ID] = true
		row := appJSON{ID: a.ID, Title: a.Title, Summary: a.Summary, Category: a.Category, State: "available", Card: a.NeedsSecret}
		if a.NeedsSecret {
			// Honest about scope: the key lives with the Bot that took it, so only those Bots can use it.
			row.Scope = keyPerBotNote
		}
		switch {
		case a.Kind == link.AppSoon:
			row.State = "soon"
		case a.Kind == link.AppNative && have[a.Grant], a.Kind == link.AppServer && serverReady(srv, a.ID, keyHeld):
			row.State = "connected"
		}
		rows = append(rows, row)
	}
	for _, v := range servers {
		if known[v.Name] {
			continue
		}
		rows = append(rows, appJSON{ID: v.Name, Title: v.Name, Summary: "自定义应用", Category: "custom", State: "connected", Custom: true})
	}
	return rows, nil
}

// serverReady says a store server is really connected: its entry exists and,
// for a key-bearing row, names every key the catalog asks for and each key has
// a value where the running server reads it. A name alone is not a key, so an
// entry without a stored value, or one left keyless by an older one-click add,
// is not shown as connected.
func serverReady(srv map[string]link.MCPServer, id string, held func(string) bool) bool {
	v, ok := srv[id]
	if !ok {
		return false
	}
	c, err := link.LookupConnector(id)
	if err != nil {
		return true
	}
	for _, name := range c.SecretEnvs {
		if !listHas(v.SecretEnvs, name) || !held(name) {
			return false
		}
	}
	return true
}

// ownerKeyHeld reports whether a key has a non-empty value where a spawned
// server looks for it: a Bot's secrets file (links install puts it there),
// then the process environment. Values are only tested, never returned.
func (s *Server) ownerKeyHeld(cfgs []bot.Config) func(string) bool {
	var files []map[string]string
	loaded := false
	return func(name string) bool {
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
			return true
		}
		if !loaded {
			loaded = true
			for _, cfg := range cfgs {
				b, err := bot.Load(s.Root, cfg.Name)
				if err != nil {
					continue
				}
				if m, err := link.ReadSecrets(b.SecretPath()); err == nil {
					files = append(files, m)
				}
			}
		}
		for _, m := range files {
			if strings.TrimSpace(m[name]) != "" {
				return true
			}
		}
		return false
	}
}

func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	a, ok := link.LookupApp(in.ID)
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	switch {
	case a.Kind == link.AppSoon:
		writeErr(w, http.StatusConflict, "即将支持")
		return
	case a.NeedsSecret:
		// The key goes through the confirmation card in chat, never this request.
		writeErr(w, http.StatusConflict, link.ErrSecrets.Error())
		return
	case a.Kind == link.AppNative:
		if err := bot.GrantGlobal(s.Root, a.Grant); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		if err := link.EnableConnector(bot.GlobalMCPPath(s.Root), a.ID); err != nil {
			writeMCPErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID, "state": "connected"})
}

func (s *Server) removeApp(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	id := r.PathValue("id")
	a, ok := link.LookupApp(id)
	switch {
	case ok && a.Kind == link.AppNative:
		if err := bot.RevokeGlobal(s.Root, a.Grant); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if a.NeedsSecret {
			if err := s.dropFromBots(a); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	case ok && a.Kind == link.AppSoon:
	default:
		if err := link.RemoveMCP(bot.GlobalMCPPath(s.Root), id); err != nil && !errors.Is(err, link.ErrMCPMissing) {
			writeMCPErr(w, err)
			return
		} else if err != nil && !ok {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if ok && a.NeedsSecret {
			if err := s.dropServerKeys(a.ID); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "state": "available"})
}

// dropFromBots takes a key-bearing connector off every bot of the owner and
// removes the credential each one stored.
func (s *Server) dropFromBots(a link.App) error {
	e, _ := link.Lookup(a.ID)
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil || !listHas(cfg.Links, a.Grant) {
			continue
		}
		if err := b.RevokeLink(a.Grant); err != nil {
			return err
		}
		if err := dropSecrets(b, e); err != nil {
			return err
		}
	}
	return nil
}

// dropServerKeys removes a key-bearing store server's keys from every bot of
// the owner; links install kept them in the installing Bot's secrets file.
func (s *Server) dropServerKeys(id string) error {
	c, err := link.LookupConnector(id)
	if err != nil {
		return nil
	}
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			continue
		}
		if err := link.DropSecrets(b.SecretPath(), c.SecretEnvs); err != nil {
			return err
		}
	}
	return nil
}

func listHas(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// addCustomApp registers an app the owner points at: a program on this
// computer or a web address. The fields are the owner's own words; the
// record is the same one the store writes for a built-in app.
func (s *Server) addCustomApp(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Name    string   `json:"name"`
		Program string   `json:"program"`
		Options []string `json:"options"`
		URL     string   `json:"url"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	name := strings.TrimSpace(in.Name)
	if _, taken := link.LookupApp(name); taken {
		writeErr(w, http.StatusConflict, "这个名字已被商店里的应用占用")
		return
	}
	spec := link.MCPServer{Name: name, Command: strings.TrimSpace(in.Program), Args: in.Options, URL: strings.TrimSpace(in.URL)}
	if (spec.Command == "") == (spec.URL == "") {
		writeErr(w, http.StatusBadRequest, "本机程序和网址请二选一")
		return
	}
	if err := link.AddMCP(bot.GlobalMCPPath(s.Root), spec); err != nil {
		writeMCPErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": name, "state": "connected"})
}
