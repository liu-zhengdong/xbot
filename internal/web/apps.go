package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/desktop"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/peer"
	"xbot/internal/vault"
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
	// Card means connecting asks for a key in settings.
	Card bool `json:"card,omitempty"`
	// Fields are the plain labels of the keys the layer asks for, in catalog order.
	Fields []string `json:"fields,omitempty"`
	Custom bool     `json:"custom,omitempty"`
	// Runtime is per Bot. Legacy state=connected describes installation only.
	Runtime map[string]link.MCPStatus `json:"runtime,omitempty"`
	LoadID  string                    `json:"load_id,omitempty"`
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
	// Snapshot references only; querying the store must never start MCP processes.
	s.mu.Lock()
	kernels := make(map[string]*kernel.Kernel, len(cfgs))
	for _, cfg := range cfgs {
		kernels[cfg.Name] = s.kernels[cfg.Name]
	}
	s.mu.Unlock()
	runtime := func(spec link.MCPServer) map[string]link.MCPStatus {
		states := make(map[string]link.MCPStatus, len(cfgs))
		for _, cfg := range cfgs {
			state := link.MCPStatus{State: "configured"}
			if k := kernels[cfg.Name]; k != nil {
				state = k.MCPStatus(spec)
			}
			states[cfg.Name] = state
		}
		return states
	}
	rows := make([]appJSON, 0, len(apps)+len(servers))
	known := map[string]bool{}
	for _, a := range apps {
		known[a.ID] = true
		row := appJSON{ID: a.ID, Title: a.Title, Summary: a.Summary, Category: a.Category, State: "available", Card: a.NeedsSecret}
		if a.NeedsSecret {
			// Honest about scope: the key lives with the Bot that took it, so only those Bots can use it.
			row.Scope = keyPerBotNote
			for _, name := range appSecretNames(a) {
				label := secretLabels[name]
				if label == "" {
					label = "密钥"
				}
				row.Fields = append(row.Fields, label)
			}
		}
		switch {
		case a.Kind == link.AppSoon:
			row.State = "soon"
		case a.Kind == link.AppNative && have[a.Grant], a.Kind == link.AppServer && serverReady(srv, a.ID, keyHeld):
			row.State = "connected"
		}
		if a.Kind == link.AppServer {
			row.LoadID = "mcp:" + a.ID
			if spec, ok := srv[a.ID]; ok {
				row.Runtime = runtime(spec)
			}
		}
		rows = append(rows, row)
	}
	for _, v := range servers {
		if known[v.Name] {
			continue
		}
		rows = append(rows, appJSON{ID: v.Name, Title: v.Name, Summary: "自定义应用", Category: "custom", State: "connected", Custom: true, Runtime: runtime(v), LoadID: "mcp:" + v.Name})
	}
	return rows, nil
}

// serverReady says a store server is really connected: its entry exists and,
// for a key-bearing row, names every key the catalog asks for, and some Bot of
// the owner would start it with every key non-blank. A name alone is not a
// key, so an entry without stored values, or one left keyless by an older
// one-click add, is not shown as connected.
func serverReady(srv map[string]link.MCPServer, id string, held func([]string) bool) bool {
	v, ok := srv[id]
	if !ok {
		return false
	}
	c, err := link.LookupConnector(id)
	if err != nil {
		return true
	}
	return link.HasSecretEnvs(v, c.SecretEnvs) && held(c.SecretEnvs)
}

// ownerKeyHeld asks, Bot by Bot, what that Bot's server would read at start:
// the same lookup and blank rule as the spawn (its secrets file first, the
// process environment only when the file has no value). Any one Bot with every
// key non-blank is enough. Values are only tested, never returned.
func (s *Server) ownerKeyHeld(cfgs []bot.Config) func([]string) bool {
	return func(names []string) bool {
		if len(names) == 0 {
			return true
		}
		for _, cfg := range cfgs {
			b, err := bot.Load(s.Root, cfg.Name)
			if err != nil {
				continue
			}
			probe := link.MCPServer{SecretEnvs: names, Secret: link.SecretLookup(b.SecretPath())}
			if probe.HasSecretValues(names) {
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
		ID      string   `json:"id"`
		Secrets []string `json:"secrets,omitempty"`
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
	s.appsMu.Lock()
	defer s.appsMu.Unlock()
	switch {
	case a.Kind == link.AppSoon:
		writeErr(w, http.StatusConflict, "即将支持")
		return
	}
	names := appSecretNames(a)
	if (a.NeedsSecret && len(names) == 0) || len(in.Secrets) != len(names) {
		writeErr(w, http.StatusBadRequest, "请填写需要的密钥。")
		return
	}
	for _, value := range in.Secrets {
		if strings.TrimSpace(value) == "" || len(value) > 4096 {
			writeErr(w, http.StatusBadRequest, "请填写需要的密钥。")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	probe := s.appsProbe
	if probe == nil {
		probe = s.probeApp
	}
	if err := probe(ctx, a, in.Secrets); err != nil || ctx.Err() != nil {
		if a.ID == "lark" && s.appsProbe == nil {
			writeErr(w, http.StatusUnprocessableEntity, "飞书暂时无法验证，请稍后再试。")
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, "没能连上，请检查后重试。")
		return
	}
	if err := s.saveApp(a, names, in.Secrets); err != nil {
		writeErr(w, http.StatusInternalServerError, "没能添加，请稍后再试。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID, "state": "connected"})
}

// secretLabels names catalog keys for people; the variable names stay off the page.
var secretLabels = map[string]string{
	"CONTEXT7_API_KEY":   "API 密钥",
	"TELEGRAM_BOT_TOKEN": "Bot Token",
	"LARK_APP_SECRET":    "应用密钥",
	"MCP_TOKEN":          "访问令牌",
}

func appSecretNames(a link.App) []string {
	if !a.NeedsSecret {
		return nil
	}
	if a.Kind == link.AppNative {
		return []string{link.SecretName(a.Grant)}
	}
	c, err := link.LookupConnector(a.ID)
	if err != nil {
		return nil
	}
	return c.SecretEnvs
}

// probeApp uses only read operations. A webhook token cannot be validated by
// sending an empty message: Feishu may still deliver it to a group.
func (s *Server) probeApp(ctx context.Context, a link.App, values []string) error {
	switch a.ID {
	case "desktop":
		path, err := desktop.Perform(ctx, s.Root, nil, desktop.Action{Name: "screenshot"}, true)
		if path != "" {
			_ = os.Remove(path)
		}
		return err
	case "browser":
		mode, err := machine.Get(s.Root, machine.Local)
		if err != nil || mode == machine.Off {
			return errors.New("machine unavailable")
		}
		root, err := os.MkdirTemp("", "xbot-browser-probe-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(root)
		sess, err := browser.Launch(ctx, root, "http://127.0.0.1/", true)
		if err != nil {
			return err
		}
		defer sess.Close()
		_, err = sess.Snapshot(ctx)
		return err
	case "lark":
		return probeLark(ctx, link.LarkAPI, values[0])
	case "telegram":
		if strings.ContainsAny(values[0], "/?#\r\n") {
			return errors.New("invalid credential")
		}
		return probeHTTP(ctx, "https://api.telegram.org/bot"+url.PathEscape(values[0])+"/getMe", "", true)
	case "context7":
		if err := probeHTTP(ctx, "https://context7.com/api/v2/libs/search?libraryName=react&query=react", values[0], false); err != nil {
			return err
		}
	}
	if a.Kind != link.AppServer {
		return errors.New("unsupported app")
	}
	c, err := link.LookupConnector(a.ID)
	if err != nil {
		return err
	}
	spec := link.MCPServer{Name: c.ID, Command: c.Command, Args: c.Args, URL: c.URL, SecretEnvs: c.SecretEnvs, Secret: func(name string) (string, bool) {
		for i, n := range c.SecretEnvs {
			if n == name {
				return values[i], true
			}
		}
		return "", false
	}}
	m := link.NewMCP(spec)
	defer m.Close()
	list, err := m.LoadTools(ctx)
	if err != nil || a.ID != "playwright" {
		return err
	}
	// A handshake proves npx ran; only a page load proves a browser came up.
	call := func(name, args string) error {
		for _, t := range list {
			if strings.HasSuffix(t.Name(), "_"+name) {
				_, err := t.Run(ctx, json.RawMessage(args))
				return err
			}
		}
		return errors.New("missing " + name)
	}
	if err := call("browser_navigate", `{"url":"about:blank"}`); err != nil {
		return err
	}
	_ = call("browser_close", `{}`)
	return nil
}

// probeLark posts a message type Feishu does not have, so nothing can reach a
// group. Feishu checks the hook token first (19001), then the body; signed and
// IP-restricted hooks (19021, 19022) would refuse every later send as well.
func probeLark(ctx context.Context, base, token string) error {
	if strings.ContainsAny(token, "/?#\r\n") {
		return errors.New("invalid credential")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/open-apis/bot/v2/hook/"+url.PathEscape(token), strings.NewReader(`{"msg_type":"xbot_probe"}`))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("probe failed")
	}
	defer res.Body.Close()
	var out struct {
		Code *int `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out); err != nil || out.Code == nil {
		return errors.New("probe rejected")
	}
	switch *out.Code {
	case 19001, 19021, 19022:
		return errors.New("probe rejected")
	}
	return nil
}

func probeHTTP(ctx context.Context, address, bearer string, telegram bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("probe failed") // the error text carries the URL, and a Telegram token sits in it
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("probe rejected")
	}
	if telegram {
		var result struct {
			OK bool `json:"ok"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil || !result.OK {
			return errors.New("probe rejected")
		}
	}
	return nil
}

func (s *Server) saveApp(a link.App, names, values []string) error {
	if len(names) == 0 {
		if a.Kind == link.AppNative {
			return bot.GrantGlobal(s.Root, a.Grant)
		}
		return link.EnableConnector(bot.GlobalMCPPath(s.Root), a.ID)
	}
	cfgs, err := peer.OwnerBots(s.Root, s.owner)
	if err != nil || len(cfgs) == 0 {
		return errors.New("no owner bot")
	}
	chosen := cfgs[0].Name
	if primary, err := bot.Primary(s.Root); err == nil {
		for _, cfg := range cfgs {
			if cfg.Name == primary {
				chosen = primary
				break
			}
		}
	}
	b, err := bot.Load(s.Root, chosen)
	if err != nil {
		return err
	}
	if a.Kind == link.AppNative {
		backend, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
		if err != nil {
			return err
		}
		already := listHas(b.Config().Links, a.Grant)
		if err := link.SetConnectorSecret(backend, a.Grant, values[0]); err != nil {
			return err
		}
		if err := b.GrantLink(a.Grant); err != nil {
			if !already {
				_ = link.RemoveConnectorSecret(backend, a.Grant)
			}
			return err
		}
		return nil
	}
	previous, err := link.ReadSecrets(b.SecretPath())
	if err != nil {
		return err
	}
	updates := make(map[string]string, len(names))
	for i, name := range names {
		updates[name] = values[i]
	}
	if err := link.WriteSecrets(b.SecretPath(), updates); err != nil {
		return err
	}
	if err := link.EnableConnector(bot.GlobalMCPPath(s.Root), a.ID); err != nil {
		_ = link.DropSecrets(b.SecretPath(), names)
		_ = link.WriteSecrets(b.SecretPath(), previous)
		return fmt.Errorf("install failed")
	}
	return nil
}

func (s *Server) removeApp(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	s.appsMu.Lock()
	defer s.appsMu.Unlock()
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
