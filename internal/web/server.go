// Package web serves the owner chat UI and the JSON API on the same port as webhooks.
package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/provider"
)

//go:embed static
var staticFS embed.FS

// Server is the web link. One process keeps a kernel per bot so subagent control stays attached.
type Server struct {
	Root         string
	Owner        string
	DefaultLinks []string
	NewKernel    func(*bot.Bot) (*kernel.Kernel, error)

	// ForkTurns lets an owner message sent mid-turn move the running turn to the
	// background and get its own answer at once. See docs/atlas/fork.md.
	ForkTurns bool

	owner       string
	token       string
	launchCodes map[string]time.Time
	launchNow   func() time.Time

	// ApprovalWait is how long an approval card waits. Zero uses 24 hours.
	ApprovalWait time.Duration

	mu        sync.Mutex
	kernels   map[string]*kernel.Kernel
	gate      map[string]*sync.Mutex
	holds     map[string]*handleHold
	approvals map[string]*pendingApproval
	// expired keeps cards that timed out with no decision, so a late answer is
	// told 410 rather than 404. Entries age out.
	expired map[string]*pendingApproval

	// nodeStatuses caches each registered node's last status list so a dead
	// node never slows /api/bots/status.
	nodeStatuses map[string]*nodeStatusCache

	// pushHTTP sends Web Push. Tests replace it. Nil uses a short timeout.
	pushHTTP *http.Client
	tabs     map[string]*tabState

	// drainMu guards foreground turn draining for a graceful restart. While
	// draining is set no new turn starts; Drain waits for inflight to hit 0.
	drainMu  sync.Mutex
	draining bool
	inflight int
}

type approvalDecision struct {
	allow   bool
	secrets map[string]string
}

// Register mounts the UI and /api routes.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		for _, accept := range strings.Split(r.Header.Get("Accept"), ",") {
			mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(accept))
			if err == nil && mediaType == "text/html" && params["q"] != "0" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>没有这个页面 · XBot</title><link rel="stylesheet" href="/app.css"><body><main class="gate"><div class="gate-card"><h1>没有这个页面</h1><a class="secondary not-found-back" href="/">回到对话</a></div></main></body></html>`)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /{$}", s.static("static/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", s.static("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /app.css", s.static("static/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /sw.js", s.static("static/sw.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /manifest.webmanifest", s.static("static/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /icon-192.png", s.static("static/icon-192.png", "image/png"))
	mux.HandleFunc("GET /icon-512.png", s.static("static/icon-512.png", "image/png"))
	mux.HandleFunc("GET /fonts/MonaSansVF.woff2", s.static("static/fonts/MonaSansVF.woff2", "font/woff2"))
	mux.HandleFunc("GET /fonts/NotoSansSC-subset.woff2", s.static("static/fonts/NotoSansSC-subset.woff2", "font/woff2"))

	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /app/launch", s.launch)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/session", s.session)

	mux.HandleFunc("GET /api/models", s.listModels)
	mux.HandleFunc("GET /api/bots", s.listBots)
	s.registerStatusAPI(mux)
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
	mux.HandleFunc("GET /api/bots/{name}/skills", s.listSkills)
	mux.HandleFunc("GET /api/bots/{name}/skills/{slug}", s.readSkill)
	mux.HandleFunc("POST /api/bots/{name}/skills/{slug}/disable", s.disableSkill)
	mux.HandleFunc("POST /api/bots/{name}/skills/{slug}/enable", s.enableSkill)
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
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/queue", func(w http.ResponseWriter, r *http.Request) { s.control(w, r, "queue") })
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/interrupt", func(w http.ResponseWriter, r *http.Request) { s.control(w, r, "interrupt") })
	mux.HandleFunc("GET /api/bots/{name}/backgrounds", s.backgrounds)
	mux.HandleFunc("DELETE /api/bots/{name}/queue/{qid}", s.cancelQueued)

	mux.HandleFunc("GET /api/notes", s.notes)
	mux.HandleFunc("POST /api/notes/read", s.readNotes)
	mux.HandleFunc("GET /api/node/pending", s.listNodePending)
	mux.HandleFunc("POST /api/node/pending/{id}", s.decideNodePending)
	mux.HandleFunc("GET /api/push", s.pushStatus)
	mux.HandleFunc("POST /api/push/subscribe", s.pushSubscribe)
	mux.HandleFunc("DELETE /api/push/subscribe", s.pushUnsubscribe)
	notify.Bind(s.Root, s.onNote)

	mux.HandleFunc("GET /api/groups", s.listGroups)
	mux.HandleFunc("POST /api/groups", s.createGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", s.patchFolder)
	mux.HandleFunc("DELETE /api/groups/{id}", s.deleteFolder)
	mux.HandleFunc("GET /api/groups/{id}", s.groupTranscript)
	mux.HandleFunc("POST /api/groups/{id}/messages", s.postGroup)
}

func (s *Server) static(name, contentType string) http.HandlerFunc {
	body, err := staticFS.ReadFile(name)
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		// no-cache still revalidates every load; the ETag turns that into a
		// 304 instead of re-sending the 2 MB CJK font over a tunnel.
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
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
		writeJSON(w, http.StatusOK, map[string]bool{"authed": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authed": true, "owner": s.owner})
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
	s.touchTab(s.owner)
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
			Unread: count > cur[cfg.Name],
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
			"id": rt.ID, "name": rt.Name, "cron": rt.Cron, "at": rt.At, "enabled": rt.Enabled, "zone": rt.Zone,
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
			"summary": clip(meta.Summary, 2000),
		})
	}
	primary, _ := bot.Primary(s.Root)
	url, bg, ink, initial := paintBot(b)
	providerName, modelName := runningModel(s.Root, b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": b.Name(), "persona": persona,
		"description": b.Config().Description,
		"style":       b.StyleCopy(),
		"quiet_start": b.Config().QuietStart,
		"quiet_end":   b.Config().QuietEnd,
		"timezone":    b.Config().Timezone,
		"avatar_url":  url, "avatar_bg": bg, "avatar_ink": ink, "initial": initial,
		"provider": providerName, "model": modelName,
		"pinned": b.Config().Pinned, "primary": b.Name() == primary,
		"group":  b.Config().Group,
		"memory": mem, "routines": routines, "agents": agents,
	})
}

// runningModel is the provider and model this bot will call.
// An empty binding resolves through the process environment, which defaults to deepseek-flash.
func runningModel(root string, b *bot.Bot) (string, string) {
	providerName := strings.TrimSpace(b.Config().Provider)
	modelName := strings.TrimSpace(b.Config().Model)
	resolved, _, err := config.Resolve(root, providerName, modelName, config.Getenv)
	if err == nil && strings.TrimSpace(resolved.Model) != "" {
		return providerName, resolved.Model
	}
	if modelName == "" {
		modelName = config.FromEnv(config.Getenv).Model
	}
	return providerName, modelName
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
	return out
}

type apiRow struct {
	Seq        int                 `json:"seq"`
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	Name       string              `json:"name,omitempty"`
	Title      string              `json:"title,omitempty"`
	Subtitle   string              `json:"subtitle,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Time       time.Time           `json:"time"`
	ReplyTo    int                 `json:"reply_to,omitempty"`
	Branch     string              `json:"branch,omitempty"`
	Kind       string              `json:"kind,omitempty"`
	From       int                 `json:"from,omitempty"`
	Task       string              `json:"task,omitempty"`
	Status     string              `json:"status,omitempty"`
	Steps      int                 `json:"steps,omitempty"`
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
	seqEntries := make([]bot.SeqEntry, len(entries))
	for i, e := range entries {
		seqEntries[i] = bot.SeqEntry{Seq: i + 1, Entry: e}
	}
	replay, _ := bot.Replay(seqEntries)
	var rows []apiRow
	for i, e := range entries {
		row := apiRow{
			Seq: i + 1, Role: e.Message.Role, Content: e.Message.Content, Name: e.Message.Name,
			ToolCalls: e.Message.ToolCalls, ToolCallID: e.Message.ToolCallID, Time: e.Time,
			ReplyTo: e.ReplyTo, Branch: replay[i].Branch,
		}
		if e.Mark != nil {
			row.Kind, row.From, row.Task = e.Mark.Kind, e.Mark.From, e.Mark.Task
			row.Status, row.Steps = e.Mark.Status, e.Mark.Steps
		}
		if card, ok := bot.ParseApprovalCard(e.Message.Content); ok {
			row.Title, row.Subtitle = card.Title, card.Subtitle
		}
		rows = append(rows, row)
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

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	// A graceful restart stops new turns; one already running keeps its SSE stream.
	if !s.BeginTurn() {
		writeErr(w, http.StatusServiceUnavailable, "restarting")
		return
	}
	defer s.EndTurn()
	s.enterTab(s.owner)
	defer s.leaveTab(s.owner)
	var in struct {
		Text    string `json:"text"`
		ReplyTo *int   `json:"reply_to"`
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
	replyTo := 0
	if in.ReplyTo != nil {
		n, err := b.Count()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if *in.ReplyTo < 1 || *in.ReplyTo > n {
			writeErr(w, http.StatusBadRequest, "bad reply_to")
			return
		}
		replyTo = *in.ReplyTo
	}
	ev := link.WebEvent(s.owner, text, time.Now())
	ev.ReplyTo = replyTo
	var (
		shared bool
		ok     bool
		lock   *fsutil.FileLock
	)
	if s.ForkTurns {
		shared, ok, err = s.acquireHandle(b)
	} else {
		lock, ok, err = b.TryLockHandle()
	}
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
	k, gate, err := s.kernel(b)
	if s.ForkTurns {
		defer s.releaseHandle(b, k)
	} else {
		defer lock.Release()
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var box *inbox.Inbox
	if shared && len(k.Backgrounds()) >= k.ForkCapValue() && k.QueueFull() {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("队列已满（%d 个），没有建任务", kernel.QueueCap))
		return
	}
	if shared {
		// A turn is running here. Post the message so that turn can fork, then answer
		// it as the foreground once the gate is free.
		box, err = inbox.Open(b.InboxPath())
		if err == nil {
			err = box.Post(ev)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
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
	if box != nil {
		defer func() { _ = box.Ack(ev.ID) }()
		if absorbed, text, bg, failed := s.absorbedReply(b, box, ev.ID, text); absorbed {
			// The running turn took the message in before it could fork.
			switch {
			case bg != "":
				send("done", doneBody{Background: bg})
			case failed:
				send("error", map[string]string{"error": "消息已被正在进行的一轮接收，但这一轮没有给出回复"})
			default:
				send("done", doneBody{Text: text})
			}
			return
		}
	}
	before := map[string]bool{}
	for _, bg := range k.Backgrounds() {
		before[bg.ID] = true
	}
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
	k.Approve = s.webApprove(b, k, sendLocked)
	err = k.Handle(r.Context(), ev)
	k.Approve = prevApprove
	k.OnDelta, k.OnTool, k.OnResult = nil, nil, nil
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	for _, bg := range k.Backgrounds() {
		if !before[bg.ID] {
			// This turn moved to the background; its result comes back as an event.
			send("done", doneBody{Background: bg.ID})
			return
		}
	}
	reply := seg.String()
	if bot.SilentText(reply) {
		reply = ""
	}
	send("done", doneBody{Text: reply, ReplyTo: k.ReplySeq()})
}

type doneBody struct {
	Text    string `json:"text"`
	ReplyTo int    `json:"reply_to,omitempty"`
	// Background is the bg_ id when this request's turn moved to the background.
	Background string `json:"background,omitempty"`
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
	if !strings.HasPrefix(r.PathValue("id"), "bg_") {
		// A background control only touches that turn's queue, so it never waits
		// behind the foreground holding the gate.
		gate.Lock()
		defer gate.Unlock()
		k.Bot = b
	}
	msg, err := k.ControlSubagent(r.Context(), r.PathValue("id"), kind, in.Text)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": msg})
}

func (s *Server) listNodePending(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	items, err := node.ListPending(s.Root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []node.Pending{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": items})
}

func (s *Server) decideNodePending(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	var in struct {
		Allow bool `json:"allow"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	id := r.PathValue("id")
	if !in.Allow {
		if err := node.Dismiss(s.Root, id); err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
		return
	}
	text, err := node.Approve(s.Root, id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "text": text})
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
		k.AllowFork = s.ForkTurns
		k.Statuses = statusSource{s}
		if s.ForkTurns {
			// A background's confirmation becomes a card in the transcript for the owner.
			k.BackgroundApprove = s.webApprove(b, k, func(string, any) {})
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

// backgrounds lists running background turns, the controls sent to them with
// pending or applied state, and the queue with each item's reason and position.
func (s *Server) backgrounds(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	k, _, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"backgrounds": k.Backgrounds(),
		"finished":    k.FinishedBackgrounds(),
		"controls":    k.Controls(),
		"queue":       k.Queue(),
		"queue_cap":   kernel.QueueCap,
	})
}

func (s *Server) cancelQueued(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	k, _, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg, err := k.ControlSubagent(r.Context(), "bg_", "cancel_queued", r.PathValue("qid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": msg})
}
