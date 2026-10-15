// Package web serves the owner chat UI and the JSON API on the same port as webhooks.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

//go:embed static/*
var staticFS embed.FS

// Server is the web link. One process keeps a kernel per bot so subagent control stays attached.
type Server struct {
	Root         string
	Owner        string
	DefaultLinks []string
	NewKernel    func(*bot.Bot) (*kernel.Kernel, error)

	owner string
	token string

	// ApprovalWait is how long an approval card waits. Zero uses 60 seconds.
	ApprovalWait time.Duration

	mu        sync.Mutex
	kernels   map[string]*kernel.Kernel
	gate      map[string]*sync.Mutex
	approvals map[string]chan bool
}

// Register mounts the UI and /api routes.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.static("static/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", s.static("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /app.css", s.static("static/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /sw.js", s.static("static/sw.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /manifest.webmanifest", s.static("static/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /icon-192.png", s.static("static/icon-192.png", "image/png"))
	mux.HandleFunc("GET /icon-512.png", s.static("static/icon-512.png", "image/png"))

	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/session", s.session)

	mux.HandleFunc("GET /api/bots", s.listBots)
	mux.HandleFunc("POST /api/bots", s.createBot)
	mux.HandleFunc("POST /api/bots/import", s.importTemplate)
	mux.HandleFunc("POST /api/bots/{name}/fork", s.forkBot)
	mux.HandleFunc("POST /api/bots/{name}/pin", s.pinBot)
	mux.HandleFunc("POST /api/bots/{name}/primary", s.setPrimary)
	mux.HandleFunc("DELETE /api/bots/{name}", s.deleteBot)
	mux.HandleFunc("PATCH /api/bots/{name}", s.patchBot)
	mux.HandleFunc("GET /api/bots/{name}/avatar", s.getAvatar)
	mux.HandleFunc("POST /api/bots/{name}/avatar", s.postAvatar)
	mux.HandleFunc("DELETE /api/bots/{name}/avatar", s.deleteAvatar)
	mux.HandleFunc("GET /api/bots/{name}/template", s.exportTemplate)
	mux.HandleFunc("GET /api/bots/{name}", s.botPanel)
	mux.HandleFunc("GET /api/bots/{name}/transcript", s.transcript)
	mux.HandleFunc("GET /api/bots/{name}/peers/{peer}/messages", s.peerMessages)
	mux.HandleFunc("GET /api/links", s.listCatalog)
	mux.HandleFunc("GET /api/bots/{name}/links", s.listBotLinks)
	mux.HandleFunc("POST /api/bots/{name}/links", s.installLink)
	mux.HandleFunc("DELETE /api/bots/{name}/links/{id}", s.removeLink)
	mux.HandleFunc("GET /api/bots/{name}/mcp", s.listMCP)
	mux.HandleFunc("POST /api/bots/{name}/mcp", s.addMCP)
	mux.HandleFunc("DELETE /api/bots/{name}/mcp/{server}", s.removeMCP)
	mux.HandleFunc("GET /api/bots/{name}/export", s.exportTranscript)
	mux.HandleFunc("POST /api/bots/{name}/messages", s.postMessage)
	mux.HandleFunc("POST /api/bots/{name}/approvals/{id}", s.decideApproval)
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/steer", s.steer)
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/cancel", s.cancel)

	mux.HandleFunc("GET /api/notes", s.notes)
	mux.HandleFunc("POST /api/notes/read", s.readNotes)

	mux.HandleFunc("GET /api/groups", s.listGroups)
	mux.HandleFunc("POST /api/groups", s.createGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", s.patchFolder)
	mux.HandleFunc("DELETE /api/groups/{id}", s.deleteFolder)
	mux.HandleFunc("GET /api/groups/{id}", s.groupTranscript)
	mux.HandleFunc("POST /api/groups/{id}/messages", s.postGroup)
}

func (s *Server) static(name, contentType string) http.HandlerFunc {
	body, err := staticFS.ReadFile(name)
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !allowOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if !s.checkToken(strings.TrimSpace(in.Token)) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.setCookie(w, s.token)
	writeJSON(w, http.StatusOK, map[string]string{"owner": s.owner})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !allowOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin")
		return
	}
	s.clearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	if !s.authenticated(r) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"owner": s.owner})
}

func (s *Server) require(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !allowOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin")
		return false
	}
	if !s.authenticated(r) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

type botItem struct {
	Name        string `json:"name"`
	Persona     string `json:"persona,omitempty"`
	Description string `json:"description,omitempty"`
	Unread      bool   `json:"unread"`
	Notes       int    `json:"notes"`
	Pinned      bool   `json:"pinned"`
	Primary     bool   `json:"primary"`
	Group       string `json:"group,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	AvatarBG    string `json:"avatar_bg,omitempty"`
	AvatarInk   string `json:"avatar_ink,omitempty"`
	Initial     string `json:"initial,omitempty"`
}

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	cur, _ := s.readCursor()
	primary, _ := bot.Primary(s.Root)
	cfgs = bot.Order(cfgs)
	var out []botItem
	for _, cfg := range cfgs {
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		n, _ := noteCount(b)
		count, _ := b.Count()
		url, bg, ink, initial := paintBot(b)
		item := botItem{
			Name: cfg.Name, Persona: clip(cfg.Persona, 80), Description: cfg.Description, Notes: n,
			Unread: n > 0 || count > cur[cfg.Name],
			Pinned: cfg.Pinned, Primary: cfg.Name == primary, Group: cfg.Group,
			AvatarURL: url, AvatarBG: bg, AvatarInk: ink, Initial: initial,
		}
		out = append(out, item)
	}
	if out == nil {
		out = []botItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Name    string `json:"name"`
		Persona string `json:"persona"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	links := s.DefaultLinks
	if len(links) == 0 {
		links = []string{link.NameTerminal, link.NameTools, link.NameTimer, link.NameMemory, link.NameWeb, link.NamePeers}
	}
	b, err := bot.Create(s.Root, strings.TrimSpace(in.Name), strings.TrimSpace(in.Persona), links)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := b.SetOwners([]string{s.owner}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := b.WriteRules(kernel.DefaultRules()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := bot.Primary(s.Root); errors.Is(err, bot.ErrNoPrimary) || errors.Is(err, bot.ErrNotFound) {
		all, err := bot.List(s.Root)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(all) == 1 {
			err = bot.SetPrimary(s.Root, b.Name())
		} else {
			_, _, err = bot.EnsurePrimary(s.Root, bot.Seed{Links: links, Owner: s.owner})
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": b.Name()})
}

func (s *Server) pinBot(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Pinned *bool `json:"pinned"`
	}
	if err := decode(r, &in); err != nil || in.Pinned == nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := b.SetPinned(*in.Pinned); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": b.Name(), "pinned": *in.Pinned})
}

func (s *Server) setPrimary(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	if err := bot.SetPrimary(s.Root, b.Name()); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": b.Name(), "primary": true, "pinned": true})
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	name := b.Name()
	if err := bot.Delete(s.Root, name); err != nil {
		if errors.Is(err, bot.ErrPrimary) || errors.Is(err, bot.ErrLast) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.forgetKernel(name)
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *Server) forkBot(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	src, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Name  string `json:"name"`
		Role  string `json:"role"`
		Group string `json:"group"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Role) == "" {
		writeErr(w, http.StatusBadRequest, "name and role are required")
		return
	}
	lock, ok, err := src.TryLockHandle()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "bot is busy")
		return
	}
	defer lock.Release()
	k, gate, err := s.kernel(src)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	gate.Lock()
	defer gate.Unlock()
	res, err := k.Fork(r.Context(), kernel.ForkRequest{Name: in.Name, Role: in.Role, Group: in.Group})
	if errors.Is(err, bot.ErrExists) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": res.Name, "group_id": res.GroupID})
}

func (s *Server) owned(name string) (*bot.Bot, error) {
	b, err := bot.Load(s.Root, name)
	if err != nil {
		return nil, err
	}
	if !contains(b.Config().Owners, s.owner) {
		return nil, os.ErrPermission
	}
	return b, nil
}

func (s *Server) botPanel(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	persona, _ := b.ReadPersona()
	mem := s.profile(b)
	routines := []map[string]any{}
	list, err := (&link.Timer{Dir: b.RoutineDir()}).List()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, rt := range list {
		routines = append(routines, map[string]any{
			"id": rt.ID, "name": rt.Name, "cron": rt.Cron, "at": rt.At, "enabled": rt.Enabled,
		})
	}
	agents := []map[string]any{}
	dir := filepath.Dir(b.AgentDir("none"))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, _, err := exec.ReadStatus(b.AgentDir(e.Name()))
		if err != nil {
			continue
		}
		agents = append(agents, map[string]any{
			"id": meta.ID, "status": meta.Status, "task": clip(meta.Task, 140), "mode": meta.Mode, "steps": meta.Steps,
		})
	}
	primary, _ := bot.Primary(s.Root)
	url, bg, ink, initial := paintBot(b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": b.Name(), "persona": persona,
		"description": b.Config().Description,
		"style":       b.StyleCopy(),
		"proactivity": b.Config().Proactivity,
		"quiet_start": b.Config().QuietStart,
		"quiet_end":   b.Config().QuietEnd,
		"timezone":    b.Config().Timezone,
		"avatar_url":  url, "avatar_bg": bg, "avatar_ink": ink, "initial": initial,
		"provider": b.Config().Provider, "model": b.Config().Model,
		"pinned": b.Config().Pinned, "primary": b.Name() == primary,
		"group":  b.Config().Group,
		"memory": mem, "routines": routines, "agents": agents,
	})
}

func (s *Server) profile(b *bot.Bot) []map[string]string {
	store := memory.Open(s.Root)
	ctx := context.Background()
	var out []map[string]string
	add := func(scope memory.Scope, personID string) {
		facts, err := store.Recall(ctx, memory.Query{Scope: scope, BotName: b.Name(), PersonID: personID, Tier: memory.TierProfile})
		if err != nil {
			return
		}
		for _, f := range facts {
			if len(out) >= 40 {
				return
			}
			out = append(out, map[string]string{"scope": string(f.Scope), "text": f.Text})
		}
	}
	add(memory.ScopePerson, s.owner)
	add(memory.ScopeBot, "")
	add(memory.ScopeBotPerson, s.owner)
	return out
}

type apiRow struct {
	Seq        int                 `json:"seq"`
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Time       time.Time           `json:"time"`
}

func (s *Server) transcript(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	entries, err := b.Entries()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.writeCursor(b.Name(), len(entries))
	var rows []apiRow
	for i, e := range entries {
		rows = append(rows, apiRow{
			Seq: i + 1, Role: e.Message.Role, Content: e.Message.Content, Name: e.Message.Name,
			ToolCalls: e.Message.ToolCalls, ToolCallID: e.Message.ToolCallID, Time: e.Time,
		})
	}
	if rows == nil {
		rows = []apiRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": rows})
}

func (s *Server) exportTranscript(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	q := r.URL.Query()
	format := strings.TrimSpace(q.Get("format"))
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "jsonl" {
		writeErr(w, http.StatusBadRequest, "bad format")
		return
	}
	tools, err := queryBool(q.Get("tools"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad tools")
		return
	}
	from, to, err := queryRange(q.Get("from"), q.Get("to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	opt := bot.TranscriptExport{Format: format, Tools: tools, From: from, To: to, Loc: time.Local}
	if err := bot.ValidateExport(b, opt); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ext := "md"
	ctype := "text/markdown; charset=utf-8"
	if format == "jsonl" {
		ext = "jsonl"
		ctype = "application/x-ndjson; charset=utf-8"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", b.Name()+"."+ext))
	w.WriteHeader(http.StatusOK)
	_ = bot.ExportTo(w, b, opt)
}

func queryBool(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, errors.New("bad tools")
	}
}

func queryRange(fromRaw, toRaw string) (int, int, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw == "" && toRaw == "" {
		return 0, 0, nil
	}
	if fromRaw == "" || toRaw == "" {
		return 0, 0, errors.New("from and to are set together")
	}
	from, err := strconv.Atoi(fromRaw)
	if err != nil {
		return 0, 0, errors.New("bad from")
	}
	to, err := strconv.Atoi(toRaw)
	if err != nil {
		return 0, 0, errors.New("bad to")
	}
	return from, to, nil
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || len(text) > 16<<10 {
		writeErr(w, http.StatusBadRequest, "bad message")
		return
	}
	ev := link.WebEvent(s.owner, text, time.Now())
	lock, ok, err := b.TryLockHandle()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := box.Post(ev); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
		return
	}
	defer lock.Release()
	k, gate, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fl, _ := w.(http.Flusher)
	flush := func() {
		if fl != nil {
			fl.Flush()
		}
	}
	send := func(event string, v any) {
		raw, _ := json.Marshal(v)
		_, _ = w.Write([]byte("event: " + event + "\ndata: " + string(raw) + "\n\n"))
		flush()
	}
	gate.Lock()
	defer gate.Unlock()
	k.Bot = b
	var seg strings.Builder
	sent := 0
	var writeMu sync.Mutex
	sendLocked := func(event string, v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		send(event, v)
	}
	k.OnDelta = func(t string) {
		seg.WriteString(t)
		if bot.SilentPrefix(seg.String()) {
			return
		}
		rest := seg.String()[sent:]
		sent = seg.Len()
		if rest != "" {
			sendLocked("delta", map[string]string{"text": rest})
		}
	}
	k.OnTool = func(name, summary string) {
		// Text before a tool call is status. The final done text is only the reply after the last tool.
		seg.Reset()
		sent = 0
		sendLocked("tool", map[string]string{"name": name, "summary": summary})
	}
	k.OnResult = func(name, output string) {
		sendLocked("result", map[string]string{"name": name, "output": clip(output, 4000)})
	}
	prevApprove := k.Approve
	k.Approve = s.webApprove(b.Name(), sendLocked)
	err = k.Handle(r.Context(), ev)
	k.Approve = prevApprove
	k.OnDelta, k.OnTool, k.OnResult = nil, nil, nil
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	reply := seg.String()
	if bot.SilentText(reply) {
		reply = ""
	}
	send("done", map[string]string{"text": reply})
}

func (s *Server) approvalWait() time.Duration {
	if s.ApprovalWait > 0 {
		return s.ApprovalWait
	}
	return 60 * time.Second
}

func (s *Server) webApprove(botName string, send func(string, any)) func(context.Context, string, string) (bool, error) {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		id := newApprovalID()
		ch := make(chan bool, 1)
		key := botName + "/" + id
		s.mu.Lock()
		if s.approvals == nil {
			s.approvals = map[string]chan bool{}
		}
		s.approvals[key] = ch
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.approvals, key)
			s.mu.Unlock()
		}()
		send("approval", map[string]string{"id": id, "tool": tool, "summary": summary})
		timer := time.NewTimer(s.approvalWait())
		defer timer.Stop()
		select {
		case allow := <-ch:
			return allow, nil
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func newApprovalID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Allow bool `json:"allow"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	key := b.Name() + "/" + r.PathValue("id")
	s.mu.Lock()
	ch := s.approvals[key]
	s.mu.Unlock()
	if ch == nil {
		writeErr(w, http.StatusNotFound, "no such approval")
		return
	}
	select {
	case ch <- in.Allow:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeErr(w, http.StatusConflict, "already decided")
	}
}

func (s *Server) steer(w http.ResponseWriter, r *http.Request) {
	s.control(w, r, exec.KindSteer)
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	s.control(w, r, exec.KindCancel)
}

func (s *Server) control(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if kind != exec.KindCancel {
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "bad request")
			return
		}
	}
	k, gate, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	gate.Lock()
	defer gate.Unlock()
	k.Bot = b
	msg, err := k.ControlSubagent(r.Context(), r.PathValue("id"), kind, in.Text)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": msg})
}

func (s *Server) notes(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var out []map[string]any
	for _, cfg := range cfgs {
		b, err := bot.Load(s.Root, cfg.Name)
		if err != nil {
			continue
		}
		notes, err := notify.Open(b.OutboxPath()).Unread()
		if err != nil {
			continue
		}
		for _, n := range notes {
			out = append(out, map[string]any{"id": n.ID, "bot": cfg.Name, "text": n.Text, "time": n.Time})
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": out})
}

func (s *Server) readNotes(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Bot string   `json:"bot"`
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	b, err := s.owned(in.Bot)
	if err != nil {
		writeMissing(w, err)
		return
	}
	if err := notify.Open(b.OutboxPath()).MarkRead(in.IDs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

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

func (s *Server) kernel(b *bot.Bot) (*kernel.Kernel, *sync.Mutex, error) {
	if s.NewKernel == nil {
		return nil, nil, errors.New("web: no kernel")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kernels == nil {
		s.kernels = map[string]*kernel.Kernel{}
		s.gate = map[string]*sync.Mutex{}
	}
	if s.gate[b.Name()] == nil {
		s.gate[b.Name()] = &sync.Mutex{}
	}
	k := s.kernels[b.Name()]
	if k == nil {
		var err error
		k, err = s.NewKernel(b)
		if err != nil {
			return nil, nil, err
		}
		s.kernels[b.Name()] = k
	}
	return k, s.gate[b.Name()], nil
}

func (s *Server) cursorPath() string { return filepath.Join(s.Root, "web-cursor.json") }

func (s *Server) readCursor() (map[string]int, error) {
	var m map[string]int
	raw, err := os.ReadFile(s.cursorPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]int{}
	}
	return m, nil
}

func (s *Server) writeCursor(name string, n int) error {
	return fsutil.WithLock(s.cursorPath()+".lock", func() error {
		m, err := s.readCursor()
		if err != nil {
			return err
		}
		if m[name] == n {
			return nil
		}
		m[name] = n
		raw, err := fsutil.Marshal(m)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(s.cursorPath(), raw, 0600)
	})
}

func noteCount(b *bot.Bot) (int, error) {
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		return 0, err
	}
	return len(notes), nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeMissing(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, bot.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}
