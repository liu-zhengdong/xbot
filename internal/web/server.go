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
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"xbot/internal/account"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notable"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/providerauth"
	"xbot/internal/remote"
	"xbot/internal/skill"
	"xbot/internal/upgrade"
	"xbot/internal/version"
	"xbot/internal/work"
)

//go:embed static
var staticFS embed.FS

// Server is the web link. One process keeps a kernel per bot so subagent control stays attached.
type Server struct {
	Root         string
	Owner        string
	DefaultLinks []string
	NewKernel    func(*bot.Bot) (*kernel.Kernel, error)

	// SkillHome is the home directory whose Agent, Codex and Claude skill libraries
	// the import list reads. Empty means the serving user's own home.
	SkillHome string

	// ForkTurns lets an owner message sent mid-turn move the running turn to the
	// background and get its own answer at once. See docs/atlas/fork.md.
	ForkTurns bool

	// App marks the desktop app's serve. Its only way in is the launch code
	// minted at start, so logging out would lock the owner out until a relaunch.
	App bool

	// OpenPath opens a directory in the desktop file manager. Only the desktop
	// app supplies it; a plain serve leaves it nil and the route stays 404.
	OpenPath func(string) error

	// AppTheme applies the page's theme to the desktop window's native buttons.
	// Nil accepts the theme and changes nothing; the app sets it before serving.
	AppTheme func(theme string) error

	// Updater answers the page's update panel and installs a downloaded
	// version on the owner's click. Nil leaves the panel out.
	Updater *upgrade.Updater

	// Account is the hosted Account API. Nil runs the local stand-in under
	// Root/account-api, whose mail is written to files instead of sent.
	Account account.Client
	acct    accountState

	// owner and token are written only by EnsureToken, under authMu, before
	// serving; checkToken and loginToken read them under authMu, so handlers
	// that passed checkToken see them without further locking.
	authMu      sync.RWMutex
	owner       string
	token       string
	launchCodes map[string]time.Time
	launchNow   func() time.Time

	// ApprovalWait is how long permission and question cards wait. Zero uses 30 minutes.
	ApprovalWait time.Duration

	// PollInterval is how often a held handle lock checks whether its
	// backgrounds ended. Zero uses 200ms. Tests shorten it.
	PollInterval time.Duration

	mu        sync.Mutex
	kernels   map[string]*kernel.Kernel
	gate      map[string]*sync.Mutex
	completed map[string]completionCount
	holds     map[string]*handleHold
	approvals map[string]*pendingApproval
	// expired retains completed cards for one window: manual decisions return
	// 409 on repetition, automatic permission decisions return 410.
	expired map[string]*pendingApproval

	// pushHTTP sends Web Push. Tests replace it. Nil uses a short timeout.
	pushHTTP *http.Client
	oauth    *providerauth.Manager
	// setupMu serializes the desktop first-run key setup.
	setupMu sync.Mutex
	// vendorMu serializes adding a built-in vendor and saving its key.
	vendorMu    sync.Mutex
	searchMu    sync.Mutex
	skillMu     sync.Mutex // one skill switch or persona-card pick at a time: each loads and saves the bot config
	searchProbe func(context.Context, string, string) error
	tabs        map[string]*tabState

	// drainMu guards foreground turn draining for a graceful restart. While
	// draining is set no new turn starts; Drain waits for inflight to hit 0.
	drainMu  sync.Mutex
	draining bool
	inflight int
}

type approvalDecision struct {
	allow     bool
	automatic bool
	secrets   map[string]string
}

// Register mounts the UI and /api routes.
func (s *Server) Register(mux *http.ServeMux) {
	if err := skill.Migrate(s.Root); err != nil {
		log.Printf("skill migration: %v", err)
	}
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
				fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>没有这个页面 · XBot</title><link rel="stylesheet" href="/tokens.css"><link rel="stylesheet" href="/app.css"><body><main class="gate"><div class="gate-card"><h1>没有这个页面</h1><a class="secondary not-found-back" href="/">回到对话</a></div></main></body></html>`)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /{$}", s.static("static/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", s.static("static/app.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /tokens.css", s.static("static/tokens.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /app.css", s.static("static/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /devices.js", s.static("static/devices.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /pairing.js", s.static("static/pairing.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /devices.css", s.static("static/devices.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /remotechat.js", s.static("static/remotechat.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /remotechat.css", s.static("static/remotechat.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /sw.js", s.static("static/sw.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /manifest.webmanifest", s.static("static/manifest.webmanifest", "application/manifest+json"))
	mux.HandleFunc("GET /icon-192.png", s.static("static/icon-192.png", "image/png"))
	mux.HandleFunc("GET /icon-512.png", s.static("static/icon-512.png", "image/png"))
	mux.HandleFunc("GET /icon-maskable-512.png", s.static("static/icon-maskable-512.png", "image/png"))
	mux.HandleFunc("GET /apple-touch-icon.png", s.static("static/apple-touch-icon.png", "image/png"))
	mux.HandleFunc("GET /favicon.svg", s.static("static/favicon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /favicon-32.png", s.static("static/favicon-32.png", "image/png"))
	mux.HandleFunc("GET /fonts/MonaSansVF.woff2", s.static("static/fonts/MonaSansVF.woff2", "font/woff2"))
	mux.HandleFunc("GET /fonts/NotoSansSC-subset.woff2", s.static("static/fonts/NotoSansSC-subset.woff2", "font/woff2"))
	// Fetched on demand for characters the subset lacks; never preloaded or precached.
	mux.HandleFunc("GET /fonts/NotoSansSC-full.woff2", s.static("static/fonts/NotoSansSC-full.woff2", "font/woff2"))
	// Bot portraits are drawn in the browser by this bundle; no SVG is stored or served.
	mux.HandleFunc("GET /vendor/xbot-avatar.min.js", s.static("static/vendor/xbot-avatar.min.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /vendor/LICENSES.txt", s.static("static/vendor/LICENSES.txt", "text/plain; charset=utf-8"))
	mux.HandleFunc("GET /vendor/katex/katex.min.css", s.static("static/vendor/katex/katex.min.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /vendor/katex/katex.min.js", s.static("static/vendor/katex/katex.min.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /vendor/katex/fonts/{name}", s.katexFont)

	s.registerAccount(mux)
	mux.HandleFunc("GET /api/ui/static/{path...}", s.uiStatic)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /app/launch", s.launch)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("POST /api/setup/key", s.setupKey)
	mux.HandleFunc("POST /api/app/theme", s.appTheme)

	mux.HandleFunc("GET /api/image-gen", s.getImageGen)
	mux.HandleFunc("PUT /api/image-gen", s.putImageGenSettings)
	mux.HandleFunc("GET /image-gen.js", s.static("static/image-gen.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /image-gen.css", s.static("static/image-gen.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/web-search", s.getWebSearch)
	mux.HandleFunc("PUT /api/web-search", s.putWebSearch)
	mux.HandleFunc("POST /api/web-search/key", s.testWebSearchKey)
	mux.HandleFunc("GET /web-search.js", s.static("static/web-search.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /web-search.css", s.static("static/web-search.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/permissions", s.getPermissions)
	mux.HandleFunc("PUT /api/permissions", s.putPermissions)
	mux.HandleFunc("GET /api/models", s.listModels)
	mux.HandleFunc("GET /api/providers", s.listProviders)
	mux.HandleFunc("GET /api/providers/models", s.providerModels)
	mux.HandleFunc("PUT /api/providers/models/image-generation", s.putImageGen)
	mux.HandleFunc("PUT /api/providers/{brand}/key", s.putVendorKey)
	mux.HandleFunc("POST /api/providers/{brand}/sync", s.syncVendorModels)
	mux.HandleFunc("POST /api/providers/oauth/{kind}/start", s.oauthStart)
	mux.HandleFunc("GET /api/providers/oauth/{kind}/status", s.oauthStatus)
	mux.HandleFunc("POST /api/providers/oauth/{kind}/cancel", s.oauthCancel)
	mux.HandleFunc("POST /api/providers/oauth/{kind}/logout", s.oauthLogout)
	mux.HandleFunc("PUT /api/bots/{name}/binding", s.bindModel)
	mux.HandleFunc("GET /providers.js", s.static("static/providers.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /providers.css", s.static("static/providers.css", "text/css; charset=utf-8"))
	for _, brand := range providerBrands() {
		mux.HandleFunc("GET /provider-icons/"+brand+".svg", s.static("static/provider-icons/"+brand+".svg", "image/svg+xml"))
	}
	// 技能导入来源行的 Claude 星芒；不是供应商品牌，不进 providerBrands()。
	mux.HandleFunc("GET /provider-icons/claude.svg", s.static("static/provider-icons/claude.svg", "image/svg+xml"))
	mux.HandleFunc("GET /api/bots", s.listBots)
	s.registerStatusAPI(mux)
	s.registerContextAPI(mux)
	s.registerUpdateAPI(mux)
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
	mux.HandleFunc("POST /api/bots/{name}/open-workdir", s.openWorkdir)
	mux.HandleFunc("GET /api/bots/{name}/generated", s.generatedWorkdir)
	mux.HandleFunc("GET /api/bots/{name}", s.botPanel)
	mux.HandleFunc("PATCH /api/bots/{name}/routines/{id}", s.patchRoutine)
	mux.HandleFunc("GET /api/bots/{name}/transcript", s.transcript)
	mux.HandleFunc("GET /api/bots/{name}/peers/{peer}/messages", s.peerMessages)
	mux.HandleFunc("GET /api/apps", s.listApps)
	mux.HandleFunc("POST /api/apps", s.installApp)
	mux.HandleFunc("POST /api/apps/custom", s.addCustomApp)
	mux.HandleFunc("DELETE /api/apps/{id}", s.removeApp)
	mux.HandleFunc("GET /apps.js", s.static("static/apps.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /apps.css", s.static("static/apps.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/links", s.listCatalog)
	mux.HandleFunc("GET /api/bots/{name}/links", s.listBotLinks)
	mux.HandleFunc("GET /api/skills", s.listLibrary)
	mux.HandleFunc("GET /api/skills/{slug}", s.readLibrarySkill)
	mux.HandleFunc("POST /api/skills/import", s.importSkill)
	mux.HandleFunc("GET /api/skills/sources/{id}", s.listSourceSkills)
	mux.HandleFunc("GET /skills.js", s.static("static/skills.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /skills.css", s.static("static/skills.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /api/persona-cards", s.listPersonaCards)
	mux.HandleFunc("POST /api/persona-cards", s.createPersonaCard)
	mux.HandleFunc("POST /api/persona-cards/import", s.importPersonaCard)
	mux.HandleFunc("GET /api/persona-cards/{id}", s.readPersonaCard)
	mux.HandleFunc("PUT /api/persona-cards/{id}", s.updatePersonaCard)
	mux.HandleFunc("DELETE /api/persona-cards/{id}", s.deletePersonaCard)
	mux.HandleFunc("PUT /api/bots/{name}/persona-card", s.pickPersonaCard)
	mux.HandleFunc("GET /persona-cards.js", s.static("static/persona-cards.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /persona-cards.css", s.static("static/persona-cards.css", "text/css; charset=utf-8"))
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
	// A pasted picture is uploaded first; the message then names it.
	mux.HandleFunc("POST /api/bots/{name}/paste", s.postPaste)
	mux.HandleFunc("GET /api/bots/{name}/paste/{image}", s.getPaste)
	mux.HandleFunc("POST /api/bots/{name}/approvals/{id}", s.decideApproval)
	mux.HandleFunc("POST /api/bots/{name}/questions/{id}", s.chooseQuestion)
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/steer", s.steer)
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/cancel", s.cancel)
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/queue", func(w http.ResponseWriter, r *http.Request) { s.control(w, r, "queue") })
	mux.HandleFunc("POST /api/bots/{name}/agents/{id}/interrupt", func(w http.ResponseWriter, r *http.Request) { s.control(w, r, "interrupt") })
	mux.HandleFunc("GET /api/bots/{name}/backgrounds", s.backgrounds)
	mux.HandleFunc("DELETE /api/bots/{name}/queue/{qid}", s.cancelQueued)

	mux.HandleFunc("GET /api/notes", s.notes)
	mux.HandleFunc("POST /api/notes/read", s.readNotes)
	s.registerDevicesAPI(mux)
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
	if dir := os.Getenv(devStaticEnv); dir != "" {
		return devStatic(dir, name, contentType)
	}
	body, err := staticFS.ReadFile(name)
	return embeddedStatic(body, err, contentType)
}

func embeddedStatic(body []byte, err error, contentType string) http.HandlerFunc {
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

// katexFont serves one vendored KaTeX font. Only names that really ship in
// static/vendor/katex/fonts are accepted, so the route cannot read anything
// else from the embedded FS.
func (s *Server) katexFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !strings.HasSuffix(name, ".woff2") {
		http.NotFound(w, r)
		return
	}
	if _, err := staticFS.ReadFile("static/vendor/katex/fonts/" + name); err != nil {
		http.NotFound(w, r)
		return
	}
	s.static("static/vendor/katex/fonts/"+name, "font/woff2").ServeHTTP(w, r)
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
	s.setCookie(w, s.loginToken())
	writeJSON(w, http.StatusOK, map[string]string{"owner": s.owner, "owner_name": s.ownerName()})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !allowOrigin(r) {
		writeErr(w, http.StatusForbidden, "cross-origin")
		return
	}
	if s.App {
		writeErr(w, http.StatusConflict, "桌面版不能退出登录，关闭窗口即可")
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
	out := map[string]any{"authed": true, "owner": s.owner, "owner_name": s.ownerName(), "app": s.App, "version": displayVersion(version.Version), "tip": version.Tip}
	// Reached through a tunnel or proxy: the 官网 entry, not this machine's
	// window. The sidebar stays the same; the bottom line names the account's
	// devices instead of this computer.
	out["remote"] = forwarded(r)
	if s.needsKey() {
		out["needs_key"] = true
		out["key_host"] = keyHost()
	}
	writeJSON(w, http.StatusOK, out)
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
	Avatar      string `json:"avatar"`
	AvatarSeed  string `json:"avatar_seed"`
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
		avatar, seed := b.AvatarState()
		item := botItem{
			Name: cfg.Name, Persona: clip(cfg.Persona, 80), Description: cfg.Description, Notes: n,
			Unread: count > cur[cfg.Name],
			Pinned: cfg.Pinned, Primary: cfg.Name == primary, Group: cfg.Group,
			AvatarURL: url, Avatar: avatar, AvatarSeed: seed, AvatarBG: bg, AvatarInk: ink, Initial: initial,
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
		Name       string  `json:"name"`
		Persona    string  `json:"persona"`
		AvatarSeed *string `json:"avatar_seed"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	// Check the seed before the bot exists so a bad one leaves nothing behind.
	if in.AvatarSeed != nil && !bot.ValidAvatarSeed(*in.AvatarSeed) {
		writeErr(w, http.StatusBadRequest, "bot: avatar seed must be 1–80 characters without markup or control characters")
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
	if in.AvatarSeed != nil {
		if err := b.SetAvatarSeed(*in.AvatarSeed); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := b.SetOwners([]string{s.owner}); err != nil {
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
	var lastApproval map[string]string
	dir := filepath.Dir(b.AgentDir("none"))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := exec.ReadMeta(b.AgentDir(e.Name()))
		if err != nil {
			continue
		}
		if meta.Status == "done" && meta.Mode == "fork-turn" && lastApproval == nil {
			// Replay assigns cards written before a fork to the fork's branch too.
			lastApproval = map[string]string{}
			if entries, err := b.Entries(); err == nil {
				seq := make([]bot.SeqEntry, len(entries))
				for i, e := range entries {
					seq[i] = bot.SeqEntry{Seq: i + 1, Entry: e}
				}
				rows, _ := bot.Replay(seq)
				for _, row := range rows {
					if card, ok := bot.ParseApprovalCard(row.Entry.Message.Text()); ok && row.Branch != "" {
						lastApproval[row.Branch] = card.State
					}
				}
			}
		}
		status := string(meta.Status)
		if status == "done" && meta.Mode == "fork-turn" && lastApproval[meta.ID] == bot.ApprovalDeclined {
			status = "denied"
		}
		var finishedAt any
		if !meta.FinishedAt.IsZero() {
			finishedAt = meta.FinishedAt
		}
		if finishedAt == nil && meta.Status != "running" && meta.Status != "queued" {
			if info, err := os.Stat(filepath.Join(b.AgentDir(e.Name()), "meta.json")); err == nil {
				finishedAt = info.ModTime().UTC()
			}
		}
		var startedAt any
		if !meta.StartedAt.IsZero() {
			startedAt = meta.StartedAt
		}
		agents = append(agents, map[string]any{
			"id": meta.ID, "type": meta.Type, "status": status, "task": clip(meta.Task, 140), "mode": meta.Mode, "steps": meta.Steps,
			"summary": clip(meta.Summary, 2000), "finished_at": finishedAt,
			"started_at": startedAt, "current_step": meta.CurrentStep, "notable": notable.Ordered(meta.Notable),
		})
	}
	// Live work first: running, awaiting, queued, then finished. Directory
	// order decides within a rank, so the list does not jump around.
	sort.SliceStable(agents, func(i, j int) bool {
		return agentRank(agents[i]["status"].(string)) < agentRank(agents[j]["status"].(string))
	})
	primary, _ := bot.Primary(s.Root)
	url, bg, ink, initial := paintBot(b)
	avatar, seed := b.AvatarState()
	providerName, modelName := runningModel(s.Root, b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": b.Name(), "persona": persona,
		"description":  b.Config().Description,
		"persona_card": s.pickedCard(b),
		"timezone":     b.Config().Timezone,
		"avatar_url":   url, "avatar": avatar, "avatar_seed": seed,
		"avatar_bg": bg, "avatar_ink": ink, "initial": initial,
		"provider": providerName, "model": modelName,
		"pinned": b.Config().Pinned, "primary": b.Name() == primary,
		"group":  b.Config().Group,
		"memory": mem, "routines": routines, "agents": agents,
	})
}

// openWorkdir opens the bot's working directory in the desktop file manager.
// Browsers have no such action: outside the app the route answers 404.
func (s *Server) openWorkdir(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	if !s.App {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.OpenPath == nil {
		writeErr(w, http.StatusInternalServerError, "open workdir is not configured")
		return
	}
	folder := r.URL.Query().Get("folder")
	if folder != "" && folder != work.GeneratedDir {
		writeErr(w, http.StatusBadRequest, "unknown folder")
		return
	}
	dir, _, err := work.Open(s.Root, b.Name())
	if err == nil && folder == work.GeneratedDir {
		dir, err = generatedPath(dir)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.OpenPath(dir); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// agentRank puts the work an owner should see first at the top of the panel.
func agentRank(status string) int {
	switch status {
	case "running":
		return 0
	case "awaiting":
		return 1
	case "queued":
		return 2
	default:
		return 3
	}
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
	Seq        int                    `json:"seq"`
	Role       string                 `json:"role"`
	Content    string                 `json:"content,omitempty"`
	Parts      []provider.ContentPart `json:"parts,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Subtitle   string                 `json:"subtitle,omitempty"`
	ToolCalls  []provider.ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Time       time.Time              `json:"time"`
	ReplyTo    int                    `json:"reply_to,omitempty"`
	Branch     string                 `json:"branch,omitempty"`
	// Internal marks a kernel note the owner did not say. The interface hides it.
	Internal   bool           `json:"internal,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	From       int            `json:"from,omitempty"`
	Task       string         `json:"task,omitempty"`
	Status     string         `json:"status,omitempty"`
	Steps      int            `json:"steps,omitempty"`
	FromDevice *apiFromDevice `json:"from_device,omitempty"`
	Relay      *apiRelay      `json:"relay,omitempty"`
	// Images are the pictures this line carried, as ids the chat turns back
	// into GET /api/bots/{name}/paste/{image}.
	Images []string `json:"images,omitempty"`
}

// apiFromDevice names the paired device an owner line was typed on.
type apiFromDevice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// apiRelay names the Bot that relayed a wrapped event, and the paired device
// it came from. A local relay omits the device.
type apiRelay struct {
	Bot    string `json:"bot"`
	Device string `json:"device,omitempty"`
}

// sourceInfo resolves row origins from one remote config per request. A config
// that cannot be read leaves labels and device names empty.
type sourceInfo struct {
	cfg   remote.Config
	known bool
}

func loadSources(root string) *sourceInfo {
	cfg, err := remote.Load(root)
	return &sourceInfo{cfg: cfg, known: err == nil}
}

func (src *sourceInfo) label(d remote.Device) string {
	if d.Label != "" {
		return d.Label
	}
	return d.Name
}

// fromDevice reads rchat_<deviceID>_<lineID>: an owner line typed on another
// paired device. Lines typed on this computer carry no such id.
func (src *sourceInfo) fromDevice(eventID string) *apiFromDevice {
	id, ok := strings.CutPrefix(eventID, "rchat_")
	if !ok || len(id) < 34 || id[32] != '_' {
		return nil
	}
	out := &apiFromDevice{ID: id[:32]}
	if src.known {
		if d, ok := src.cfg.ByID(out.ID); ok {
			out.Label = src.label(d)
		}
	}
	return out
}

// relay reads source bot:<bot> (local) or bot:<bot>@<device name>. It only
// parses hostnames, which admit no '@', so the two do not overlap.
func (src *sourceInfo) relay(content string) *apiRelay {
	source, _, wrapped := peer.EventSource(content)
	name, ok := strings.CutPrefix(source, "bot:")
	if !wrapped || !ok {
		return nil
	}
	botName, device, _ := strings.Cut(name, "@")
	if botName == "" {
		return nil
	}
	if device == "" {
		return &apiRelay{Bot: botName}
	}
	if src.known {
		if d, ok := src.cfg.ByName(device); ok {
			device = src.label(d)
		}
	}
	return &apiRelay{Bot: botName, Device: device}
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
	src := loadSources(s.Root)
	var rows []apiRow
	for i, e := range entries {
		row := apiRow{
			Parts: publicParts(e.Message), Seq: i + 1, Role: e.Message.Role, Content: e.Message.Text(), Name: e.Message.Name,
			ToolCalls: e.Message.Calls(), ToolCallID: e.Message.CallID(), Time: e.Time,
			ReplyTo: e.ReplyTo, Branch: replay[i].Branch, Internal: e.IsInternal(),
			Images: e.Images,
		}
		if e.Message.Role == provider.RoleUser {
			row.FromDevice, row.Relay = src.fromDevice(e.EventID), src.relay(e.Message.Text())
		}
		if e.Mark != nil {
			row.Kind, row.From, row.Task = e.Mark.Kind, e.Mark.From, e.Mark.Task
			row.Status, row.Steps = e.Mark.Status, e.Mark.Steps
		}
		if card, ok := bot.ParseApprovalCard(e.Message.Text()); ok {
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
	// Cleanups run when this request is done with the turn. A request whose
	// turn was handed to a newer message returns early and leaves them to
	// run when the turn ends.
	var cleanups []func()
	later := func(f func()) { cleanups = append(cleanups, f) }
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	detached := false
	defer func() {
		if !detached {
			cleanup()
		}
	}()
	later(s.EndTurn)
	s.enterTab(s.owner)
	later(func() { s.leaveTab(s.owner) })
	var in struct {
		Text    string   `json:"text"`
		ReplyTo *int     `json:"reply_to"`
		Images  []string `json:"images"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	text := strings.TrimSpace(in.Text)
	// A picture on its own is a message: "look at this" needs no words.
	if (text == "" && len(in.Images) == 0) || len(text) > 16<<10 {
		writeErr(w, http.StatusBadRequest, "bad message")
		return
	}
	images, err := s.checkPasted(b, in.Images)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
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
	ev.Images = images
	if !s.ForkTurns {
		// The owner's word goes straight into a running turn (ADR 0017).
		// Accepted messages get either a reply or an explicit failed outcome
		// on the transcript, including messages whose stream already handed off.
		if s.interject(w, r, b, ev) {
			return
		}
	}
	var (
		shared bool
		ok     bool
		lock   *fsutil.FileLock
		waited *inbox.Inbox
	)
	if s.ForkTurns {
		shared, ok, err = s.acquireHandle(b)
	} else {
		lock, ok, err = b.TryLockHandle()
	}
	if err != nil {
		streamErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			streamErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := box.Post(ev); err != nil {
			streamErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if s.ForkTurns {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
			return
		}
		// The running turn is in another process (a terminal chat), out of
		// reach of Interject. The message waits in the inbox, which that turn
		// reads at its next step boundary. This request stays open: once the
		// lock is free it reports that turn's answer, or answers the message
		// itself when the turn ended before reading it.
		startStream(w)
		if lock = s.waitHandle(r.Context(), b); lock == nil {
			// The page went away or serve is restarting: the message stays in
			// the inbox and serve answers it.
			return
		}
		waited = box
	}
	k, gate, err := s.kernel(b)
	if s.ForkTurns {
		later(func() { s.releaseHandle(b, k) })
	} else {
		later(func() { _ = lock.Release() })
	}
	if err != nil {
		streamErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	box := waited
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
	startStream(w)
	send := func(event string, v any) { sendEvent(w, event, v) }
	gate.Lock()
	later(gate.Unlock)
	k.Bot = b
	if box != nil {
		later(func() { _ = box.Ack(ev.ID) })
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
	rs := &replyStream{w: w}
	sendLocked := rs.send
	k.OnPart = rs.part
	k.OnDelta, k.OnTool, k.OnResult = rs.delta, rs.tool, rs.result
	prevApprove := k.Approve
	k.Approve = s.webApprove(b, k, sendLocked)
	// The turn runs on its own context: leaving the page cancels it, unless a
	// newer message took it over first, and that message's request owns it now.
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(r.Context()))
	stopWatch := context.AfterFunc(r.Context(), cancelRun)
	handoff := make(chan struct{})
	var once sync.Once
	k.OnHandoff = func() {
		once.Do(func() {
			stopWatch()
			close(handoff)
		})
	}
	later(func() {
		k.Approve = prevApprove
		k.OnPart = nil
		k.OnDelta, k.OnTool, k.OnResult, k.OnHandoff = nil, nil, nil, nil
		stopWatch()
		cancelRun()
	})
	handled := make(chan error, 1)
	go func() { handled <- k.Handle(runCtx, ev) }()
	select {
	case err = <-handled:
	case <-handoff:
		// A newer message took this turn's output; its request has the
		// answer. The locks stay held until the turn ends.
		detached = true
		rs.send("done", doneBody{HandedOff: true})
		rs.close()
		go func() {
			<-handled
			cleanup()
		}()
		return
	}
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	if k.HandedOff() {
		send("done", doneBody{HandedOff: true})
		return
	}
	for _, bg := range k.Backgrounds() {
		if !before[bg.ID] {
			// This turn moved to the background; its result comes back as an event.
			send("done", doneBody{Background: bg.ID})
			return
		}
	}
	send("done", doneBody{Text: rs.reply(), ReplyTo: k.ReplySeq()})
}

// interject hands ev to the bot's running turn in this process and streams
// that turn's answer back on this request. False means no live turn accepted
// it. The kernel records each accepted request's failed outcome on errors.
func (s *Server) interject(w http.ResponseWriter, r *http.Request, b *bot.Bot, ev link.Event) bool {
	rs := &replyStream{w: w}
	handoff := make(chan struct{})
	var once sync.Once
	sink := &kernel.Sink{
		OnPart: rs.part, OnDelta: rs.delta, OnTool: rs.tool, OnResult: rs.result,
		OnHandoff: func() { once.Do(func() { close(handoff) }) },
		Done:      make(chan kernel.Outcome, 1),
	}
	// The stream starts before the turn can write to it.
	rs.mu.Lock()
	if !kernel.Interject(b, ev, sink) {
		rs.mu.Unlock()
		return false
	}
	startStream(w)
	rs.mu.Unlock()
	// The page leaving does not stop the turn: the answer is on the
	// transcript. Nothing is written here after this request returns.
	defer rs.close()
	select {
	case <-r.Context().Done():
		return true
	case <-handoff:
		rs.send("done", doneBody{HandedOff: true})
		return true
	case out := <-sink.Done:
		switch {
		case out.Err != nil:
			rs.send("error", map[string]string{"error": out.Err.Error()})
		default:
			rs.send("done", doneBody{Text: rs.reply(), ReplyTo: out.ReplyTo})
		}
		return true
	}
}

// replyStream writes one request's live answer as server-sent events. Text
// before a tool call is status; reply is only what came after the last tool.
type replyStream struct {
	w      http.ResponseWriter
	mu     sync.Mutex
	seg    strings.Builder
	sent   int
	closed bool
}

func (rs *replyStream) send(event string, v any) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.closed {
		sendEvent(rs.w, event, v)
	}
}

func (rs *replyStream) delta(t string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.seg.WriteString(t)
	if rs.closed || bot.SilentPrefix(rs.seg.String()) {
		return
	}
	rest := rs.seg.String()[rs.sent:]
	rs.sent = rs.seg.Len()
	if rest != "" {
		sendEvent(rs.w, "delta", map[string]string{"text": rest})
	}
}

func (rs *replyStream) tool(name, summary string) {
	rs.mu.Lock()
	rs.seg.Reset()
	rs.sent = 0
	rs.mu.Unlock()
	rs.send("tool", map[string]string{"name": name, "summary": summary})
}

func (rs *replyStream) result(name, output string) {
	rs.send("result", map[string]string{"name": name, "output": clip(output, 4000)})
}

func (rs *replyStream) reply() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	reply := rs.seg.String()
	if bot.SilentText(reply) {
		return ""
	}
	return reply
}

// close stops all later writes, for a request that has returned.
func (rs *replyStream) close() {
	rs.mu.Lock()
	rs.closed = true
	rs.mu.Unlock()
}

// streamErr reports err as an error event once the stream has started, and
// as a plain error response before that.
func streamErr(w http.ResponseWriter, code int, msg string) {
	if w.Header().Get("Content-Type") == "text/event-stream" {
		sendEvent(w, "error", map[string]string{"error": msg})
		return
	}
	writeErr(w, code, msg)
}

// startStream sends the event-stream headers now, so the page knows the
// message was taken before any answer exists. A second call does nothing.
func startStream(w http.ResponseWriter) {
	if w.Header().Get("Content-Type") == "text/event-stream" {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func sendEvent(w http.ResponseWriter, event string, v any) {
	raw, _ := json.Marshal(v)
	_, _ = w.Write([]byte("event: " + event + "\ndata: " + string(raw) + "\n\n"))
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

// waitHandle waits for the bot's handle lock. It returns nil when ctx ends or
// the server starts draining for a restart.
func (s *Server) waitHandle(ctx context.Context, b *bot.Bot) *fsutil.FileLock {
	tick := time.NewTicker(s.holdPoll())
	defer tick.Stop()
	for {
		lock, ok, err := b.TryLockHandle()
		if err != nil {
			return nil
		}
		if ok {
			return lock
		}
		s.drainMu.Lock()
		draining := s.draining
		s.drainMu.Unlock()
		if draining {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

type doneBody struct {
	Text    string `json:"text"`
	ReplyTo int    `json:"reply_to,omitempty"`
	// Background is the bg_ id when this request's turn moved to the background.
	Background string `json:"background,omitempty"`
	// HandedOff is set when a newer owner message took over this turn; the
	// answer arrives on that message's request.
	HandedOff bool `json:"handed_off,omitempty"`
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
		if !s.ForkTurns {
			// A late decision on a parked card wakes the bot at once.
			kernel.SetWaker(s.Root, s.wakeBot)
		}
	}
	if s.gate[b.Name()] == nil {
		s.gate[b.Name()] = &sync.Mutex{}
	}
	k := s.kernels[b.Name()]
	if k == nil {
		// A profile may have been read before a concurrent binding update.
		// Refresh under the same mutex that removes the previous cache entry.
		fresh, err := bot.Load(b.Root, b.Name())
		if err != nil {
			return nil, nil, err
		}
		*b = *fresh
		k, err = s.NewKernel(b)
		if err != nil {
			return nil, nil, err
		}
		k.ApprovalWait = s.approvalWait()
		k.AllowFork = s.ForkTurns
		k.Statuses = statusSource{s}
		if s.ForkTurns || k.Dispatch {
			// A background's or a subagent's confirmation becomes a card in the
			// transcript for the owner.
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

// wakeBot handles a bot's inbox now, for an owner decision posted outside any
// turn. When the handle lock is taken, the turn holding it reads the inbox
// at its next step, so there is nothing to do here.
func (s *Server) wakeBot(b *bot.Bot) {
	go func() {
		lock, ok, err := b.TryLockHandle()
		if err != nil || !ok {
			return
		}
		defer lock.Release()
		k, gate, err := s.kernel(b)
		if err != nil {
			return
		}
		gate.Lock()
		defer gate.Unlock()
		k.Bot = b
		_ = proc.Drain(context.Background(), k)
	}()
}

func publicParts(m provider.Message) []provider.ContentPart {
	var out []provider.ContentPart
	for _, p := range m.Content {
		if p.Type != provider.PartHosted && p.Type != provider.PartCitation {
			continue
		}
		p.VendorRaw = nil
		out = append(out, p)
	}
	return out
}

func (rs *replyStream) part(p provider.ContentPart) { rs.send("part", p) }
