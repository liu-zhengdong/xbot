package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/code"
	"xbot/internal/config"
	"xbot/internal/desktop"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// linksTool lets the bot search, install, remove, and load links.
// Install always asks the owner. Secret values arrive from the approval card,
// never from the model, and never go back out in the tool result.
type linksTool struct{ k *Kernel }

func (linksTool) Name() string { return "links" }
func (linksTool) Description() string {
	return "搜索、安装、移除或加载连接器。看到上下文‘连接器’摘要中的名称:描述后，先用 action=load、id=名称或 id 加载，例如电脑操作/desktop、浏览器/browser、Playwright/playwright/mcp:playwright。成功返回工具名，工具 schema 只加入本轮后续调用；失败返回原因。search 查摘要；install/remove 走主人确认，所需密钥由主人在确认卡填写，你不接收密钥值。"
}
func (linksTool) Parameters() any {
	return schema(map[string]any{
		"action": stringProp("search, install, remove, or load"),
		"id":     stringProp("名称或 id；load 也接受 mcp:<服务器>"),
		"query":  stringProp("search text; empty lists the catalog"),
	}, []string{"action"})
}

// Confirm only builds the card summary for install and remove. The kernel
// review path decides that install and remove need the owner.
func (t linksTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false, "", err
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	id := strings.TrimSpace(in.ID)
	switch action {
	case "install":
		e, ok := link.Lookup(id)
		if !ok {
			if c, ok := storeConnector(id); ok {
				return false, connectorCard(c), nil
			}
			return false, "install " + id, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "install %s\n%s", e.ID, linkOneLine(e.Summary))
		if len(e.Secrets) > 0 {
			fmt.Fprintf(&b, "\nsecrets: %s", strings.Join(e.Secrets, ", "))
		}
		return false, b.String(), nil
	case "remove":
		return false, "remove " + id, nil
	default:
		return false, "", nil
	}
}

func (t linksTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Query  string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if t.k == nil || t.k.Bot == nil {
		return "", fmt.Errorf("links: no bot")
	}
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "search":
		return t.search(in.Query), nil
	case "install":
		return t.install(strings.TrimSpace(in.ID))
	case "remove":
		return t.remove(strings.TrimSpace(in.ID))
	case "load":
		return t.loadContext(ctx, strings.TrimSpace(in.ID))
	default:
		return "", fmt.Errorf("links: action must be search, install, remove, or load")
	}
}

func (t linksTool) search(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	granted := map[string]bool{}
	for _, name := range t.k.Bot.EffectiveLinks() {
		granted[name] = true
	}
	var b strings.Builder
	for _, e := range link.Catalog() {
		blob := strings.ToLower(e.ID + " " + e.Title + " " + e.Summary + " " + strings.Join(e.Keywords, " "))
		if q != "" && !strings.Contains(blob, q) {
			continue
		}
		state := "listed"
		if e.Available {
			state = "available"
		}
		if e.Grant != "" && granted[e.Grant] {
			state = "installed"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s", e.ID, state, linkOneLine(e.Summary))
		if len(e.Secrets) > 0 {
			fmt.Fprintf(&b, "\tsecrets: %s", strings.Join(e.Secrets, ", "))
		}
		b.WriteByte('\n')
	}
	servers, _ := link.LoadMCPAll(t.k.Bot.GlobalMCPPath(), t.k.Bot.MCPPath())
	store := map[string]bool{}
	if list, err := link.Connectors(); err == nil {
		for _, c := range list {
			if _, ok := storeConnector(c.ID); !ok {
				continue
			}
			store[c.ID] = true
			blob := strings.ToLower(c.ID + " " + c.Title + " " + c.Summary)
			if q != "" && !strings.Contains(blob, q) {
				continue
			}
			// Installed only once the entry names every key and each has a value.
			state := "available"
			for _, s := range servers {
				s.Secret = link.SecretLookup(t.k.Bot.SecretPath())
				if s.Name == c.ID && s.HasSecretValues(c.SecretEnvs) {
					state = "installed"
				}
			}
			fmt.Fprintf(&b, "%s\t%s\t%s", c.ID, state, linkOneLine(c.Summary))
			if len(c.SecretEnvs) > 0 {
				fmt.Fprintf(&b, "\tsecrets: %s", strings.Join(c.SecretEnvs, ", "))
			}
			b.WriteByte('\n')
		}
	}
	if servers != nil {
		for _, s := range servers {
			if store[s.Name] {
				continue
			}
			id := "mcp:" + s.Name
			summary := link.Blurb(id)
			if q != "" && !strings.Contains(strings.ToLower(id+" "+summary), q) {
				continue
			}
			fmt.Fprintf(&b, "%s\tinstalled\t%s\n", id, linkOneLine(summary))
		}
	}
	text := strings.TrimRight(b.String(), "\n")
	if text == "" {
		return "no links"
	}
	return scrubSecrets(text, t.k.secretValues())
}

func (t linksTool) install(id string) (string, error) {
	e, ok := link.Lookup(id)
	if !ok {
		if c, ok := storeConnector(id); ok {
			return t.installConnector(c)
		}
		return "", fmt.Errorf("links: not found")
	}
	values := t.k.takeSecrets(e.ID)
	var keep map[string]string
	if len(e.Secrets) > 0 {
		var missing []string
		keep = map[string]string{}
		for _, name := range e.Secrets {
			v := strings.TrimSpace(values[name])
			if v == "" {
				missing = append(missing, name)
				continue
			}
			keep[name] = v
		}
		if len(missing) > 0 {
			return scrubSecrets("links: owner did not fill "+strings.Join(missing, ", "), values), nil
		}
	}
	if !e.Available || e.Grant == "" {
		return scrubSecrets("links: not available yet", values), nil
	}
	if len(keep) > 0 {
		if err := t.k.storeSecrets(keep); err != nil {
			return scrubSecrets("links: "+err.Error(), values), nil
		}
	}
	if err := t.k.Bot.GrantLink(e.Grant); err != nil {
		return scrubSecrets("links: "+err.Error(), values), nil
	}
	return scrubSecrets("installed "+e.ID, values), nil
}

// storeConnector finds a store app that runs as a catalog server, by id,
// title, or mcp:<id>. Every such row installs the same way; a row that names
// keys takes them from the approval card.
func storeConnector(id string) (link.CatalogItem, bool) {
	id = strings.TrimPrefix(link.ResolveApp(strings.TrimSpace(id)), "mcp:")
	a, ok := link.LookupApp(id)
	if !ok || a.Kind != link.AppServer {
		return link.CatalogItem{}, false
	}
	c, err := link.LookupConnector(a.ID)
	return c, err == nil
}

// connectorCard is the install summary; the approval card reads the id from
// the first line and the key names from the secrets line.
func connectorCard(c link.CatalogItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "install %s\n%s", c.ID, linkOneLine(c.Summary))
	if len(c.SecretEnvs) > 0 {
		fmt.Fprintf(&b, "\nsecrets: %s", strings.Join(c.SecretEnvs, ", "))
	}
	return b.String()
}

// installConnector writes a store server into the owner's global mcp.json.
// Its keys go to this Bot's key store first; without every key nothing is
// written, and an older keyless entry is upgraded so the key reaches it.
func (t linksTool) installConnector(c link.CatalogItem) (string, error) {
	values := t.k.takeSecrets(c.ID)
	keep := map[string]string{}
	var missing []string
	for _, name := range c.SecretEnvs {
		v := strings.TrimSpace(values[name])
		if v == "" {
			missing = append(missing, name)
			continue
		}
		keep[name] = v
	}
	if len(missing) > 0 {
		return scrubSecrets("links: owner did not fill "+strings.Join(missing, ", "), values), nil
	}
	if len(keep) > 0 {
		if err := t.k.storeSecrets(keep); err != nil {
			return scrubSecrets("links: "+err.Error(), values), nil
		}
	}
	if err := link.EnableConnector(t.k.Bot.GlobalMCPPath(), c.ID); err != nil {
		return scrubSecrets("links: "+err.Error(), values), nil
	}
	t.k.snapshotLinks() // mount it now, so load works in this turn
	return scrubSecrets("installed "+c.ID, values), nil
}

// removeConnector takes a store server out of the global and this Bot's
// mcp.json and drops the keys this Bot stored for it. Removing twice is fine.
func (t linksTool) removeConnector(c link.CatalogItem) (string, error) {
	for _, path := range []string{t.k.Bot.GlobalMCPPath(), t.k.Bot.MCPPath()} {
		if err := link.RemoveMCP(path, c.ID); err != nil && !errors.Is(err, link.ErrMCPMissing) {
			return "", err
		}
	}
	if err := t.k.dropSecrets(link.CatalogEntry{ID: c.ID, Secrets: c.SecretEnvs}); err != nil {
		return "", err
	}
	t.k.snapshotLinks()
	return "removed " + c.ID, nil
}

func (t linksTool) remove(id string) (string, error) {
	e, ok := link.Lookup(id)
	if !ok {
		if c, ok := storeConnector(id); ok {
			return t.removeConnector(c)
		}
		return "", fmt.Errorf("links: not found")
	}
	if e.Grant != "" {
		if err := t.k.Bot.RevokeLink(e.Grant); err != nil {
			return "", err
		}
	}
	if len(e.Secrets) > 0 {
		if err := t.k.dropSecrets(e); err != nil {
			return "", err
		}
	}
	return "removed " + e.ID, nil
}

func (t linksTool) load(id string) (string, error) { return t.loadContext(context.Background(), id) }

func (t linksTool) loadContext(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("links: id is required")
	}
	id = link.ResolveApp(id)
	if c, ok := storeConnector(id); ok {
		id = "mcp:" + c.ID
	}
	if strings.HasPrefix(id, "mcp:") {
		name := strings.TrimPrefix(id, "mcp:")
		servers, err := link.LoadMCPAll(t.k.Bot.GlobalMCPPath(), t.k.Bot.MCPPath())
		if err != nil {
			return "", err
		}
		found := false
		for _, s := range servers {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("links: not found")
		}
		return t.loadedNamesContext(ctx, id)
	}
	e, ok := link.Lookup(id)
	if !ok {
		return "", fmt.Errorf("links: not found")
	}
	if e.Grant == "" || !t.k.Bot.Grants(e.Grant) {
		return "", fmt.Errorf("links: not installed")
	}
	t.k.markLoaded(e.ID)
	return t.loadedNames(e.ID)
}

func (t linksTool) loadedNames(id string) (string, error) {
	return t.loadedNamesContext(context.Background(), id)
}

func (t linksTool) loadedNamesContext(ctx context.Context, id string) (string, error) {
	list, err := t.k.toolsForLoaded(ctx, id)
	if err != nil {
		return "", errors.New(t.k.redactor()(err.Error()))
	}
	names := make([]string, 0, len(list))
	for _, tool := range list {
		if tool == nil || tool.Name() == "" {
			continue
		}
		names = append(names, tool.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "loaded " + id + ": no tools", nil
	}
	t.k.markLoaded(id)
	return "loaded " + id + ": " + strings.Join(names, ", "), nil
}

func (k *Kernel) toolsForLoaded(ctx context.Context, id string) ([]tools.Tool, error) {
	links := k.snapshotLinks()
	if id == link.NameSkills || id == "skills" {
		if namedLink(links, link.NameSkills) {
			return []tools.Tool{skillTool{k}, writeSkillTool{k}}, nil
		}
		return nil, nil
	}
	var out []tools.Tool
	for _, l := range links {
		if l == nil {
			continue
		}
		if l.Name() == id || (id != "" && l.Name() == grantName(id)) {
			if m, ok := l.(*link.MCP); ok {
				list, err := m.LoadTools(ctx)
				if err != nil {
					return nil, err
				}
				out = append(out, list...)
			} else {
				out = append(out, l.Tools()...)
			}
		}
	}
	return out, nil
}

// snapshotLinks mounts catalog grants that were installed after this kernel
// started, and drops catalog grants that were removed. Runner and mcp links stay.
func (k *Kernel) snapshotLinks() []link.Link {
	if k == nil {
		return nil
	}
	k.linksMu.Lock()
	defer k.linksMu.Unlock()
	if k.Bot != nil {
		k.syncGrantsLocked()
	}
	out := make([]link.Link, len(k.Links))
	copy(out, k.Links)
	return out
}

func (k *Kernel) syncGrantsLocked() {
	granted := map[string]bool{}
	for _, name := range k.Bot.EffectiveLinks() {
		if catalogRuntime(name) {
			granted[name] = true
		}
	}
	var kept []link.Link
	have := map[string]bool{}
	for _, l := range k.Links {
		if l == nil {
			continue
		}
		name := l.Name()
		if catalogRuntime(name) && !granted[name] {
			continue
		}
		if have[name] {
			continue
		}
		kept = append(kept, l)
		have[name] = true
	}
	for _, name := range k.Bot.EffectiveLinks() {
		if !catalogRuntime(name) || have[name] {
			continue
		}
		mounted := k.mountGrantLocked(name)
		if mounted == nil {
			continue
		}
		kept = append(kept, mounted)
		have[name] = true
	}
	k.Links = k.syncMCPLocked(kept)
}

// syncMCPLocked makes the mounted mcp links match the global and personal
// mcp.json: a server the store added is mounted, one it removed is closed, and
// one whose definition changed is replaced by a fresh instance. An unreadable
// file leaves the mounted set as it is.
func (k *Kernel) syncMCPLocked(links []link.Link) []link.Link {
	servers, err := link.LoadMCPAll(k.Bot.GlobalMCPPath(), k.Bot.MCPPath())
	if err != nil {
		return links
	}
	want := make(map[string]link.MCPServer, len(servers))
	for _, s := range servers {
		want[s.Name] = s
	}
	out := make([]link.Link, 0, len(links)+len(servers))
	have := map[string]bool{}
	var stale []*link.MCP
	for _, l := range links {
		m, ok := l.(*link.MCP)
		if !ok {
			out = append(out, l)
			continue
		}
		if s, ok := want[m.Spec.Name]; ok && sameMCPSpec(m.Spec, s) {
			out = append(out, l)
			have[m.Spec.Name] = true
			continue
		}
		stale = append(stale, m)
	}
	for _, s := range servers {
		if have[s.Name] {
			continue
		}
		s.Secret = link.SecretLookup(k.Bot.SecretPath())
		mk := k.newMCP
		if mk == nil {
			mk = func(s link.MCPServer) link.Link { return link.NewMCP(s) }
		}
		out = append(out, mk(s))
	}
	for _, m := range stale {
		_ = m.Close()
	}
	return out
}

func sameMCPSpec(a, b link.MCPServer) bool {
	a.Dial, b.Dial = nil, nil
	a.Secret, b.Secret = nil, nil
	a.Args, b.Args = append([]string{}, a.Args...), append([]string{}, b.Args...)
	a.SecretEnvs, b.SecretEnvs = append([]string{}, a.SecretEnvs...), append([]string{}, b.SecretEnvs...)
	return reflect.DeepEqual(a, b)
}

func catalogRuntime(name string) bool {
	switch name {
	case link.NameWebhook, link.NameWatch, link.NameSkills, link.NameTelegram, link.NameLark, link.NameCoding, link.NameDesktop, link.NameBrowser:
		return true
	default:
		return false
	}
}

// storeSecrets writes link credentials. With a vault they go to the backend
// under the connector name; without one the legacy plaintext file keeps the
// old behaviour. Only connector secrets are vaulted: other links keep their
// file entry, so a mixed install neither loses nor leaks a value.
func (k *Kernel) storeSecrets(values map[string]string) error {
	rest := map[string]string{}
	for name, value := range values {
		kind := link.KindForSecret(name)
		if kind == "" || k.Vault == nil {
			rest[name] = value
			continue
		}
		if err := link.SetConnectorSecret(k.Vault, kind, value); err != nil {
			return err
		}
	}
	if len(rest) == 0 {
		return nil
	}
	return link.WriteSecrets(k.Bot.SecretPath(), rest)
}

// dropSecrets removes link credentials from the vault and the legacy file.
func (k *Kernel) dropSecrets(e link.CatalogEntry) error {
	for _, name := range e.Secrets {
		kind := link.KindForSecret(name)
		if kind == "" || k.Vault == nil {
			continue
		}
		if err := link.RemoveConnectorSecret(k.Vault, kind); err != nil {
			return err
		}
	}
	return link.DropSecrets(k.Bot.SecretPath(), e.Secrets)
}

// connector mounts a Telegram or Lark connector and, on this first access,
// migrates any legacy plaintext token into the vault. A bot that never touches
// the connector keeps its file until it is needed.
func (k *Kernel) connector(dir, kind string) *link.Connector {
	c := link.NewConnector(dir, k.Bot.SecretPath(), kind)
	if k == nil || k.Vault == nil {
		return c
	}
	if err := c.UseVault(k.Vault); err != nil {
		_ = err // a failed migration retries on the next mount; the file stays
	}
	return c
}

func (k *Kernel) mountGrantLocked(name string) link.Link {
	dir := bot.Dir(k.Bot.Root, k.Bot.Name())
	switch name {
	case link.NameWebhook:
		return link.Webhook{}
	case link.NameWatch:
		return link.Watch{}
	case link.NameSkills:
		return link.Skills{}
	case link.NameTelegram:
		return k.connector(dir, link.NameTelegram)
	case link.NameLark:
		return k.connector(dir, link.NameLark)
	case link.NameDesktop:
		return desktop.Open(k.Bot.Root)
	case link.NameBrowser:
		return browser.Open(k.Bot.Root)
	case link.NameCoding:
		l := code.Open(dir, k.Bot.InboxPath())
		if k.Provider != nil {
			l.UseProvider(k.boundProvider())
		}
		wired := false
		for _, existing := range k.Links {
			r, ok := existing.(link.Runner)
			if ok && strings.TrimSpace(r.BaseURL) != "" {
				l.UseRunner(r.BaseURL, r.Token)
				wired = true
				break
			}
		}
		if !wired {
			if url := strings.TrimSpace(os.Getenv("XBOT_RUNNER_URL")); url != "" {
				l.UseRunner(url, os.Getenv("XBOT_RUNNER_TOKEN"))
			}
		}
		if k.Bot != nil {
			b := k.Bot
			l.UseTranscript(func(text string) error {
				return b.Append(bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
			})
		}
		if cmd := code.ResolveCommand(os.Getenv("XBOT_CODE_AGENT")); cmd != "" {
			l.UseCommand(cmd)
		}
		return l
	default:
		return nil
	}
}

func grantName(id string) string {
	e, ok := link.Lookup(id)
	if !ok {
		return ""
	}
	return e.Grant
}

func (k *Kernel) installedSection() (string, error) {
	if k == nil || k.Bot == nil {
		return "", nil
	}
	granted := map[string]bool{}
	for _, name := range k.Bot.EffectiveLinks() {
		granted[name] = true
	}
	var refs []link.InstalledRef
	for _, e := range link.Catalog() {
		if e.Grant == "" || !granted[e.Grant] {
			continue
		}
		if a, ok := link.LookupApp(e.ID); ok {
			refs = append(refs, link.InstalledRef{ID: a.Title, Summary: a.Summary})
			continue
		}
	}
	servers, err := link.LoadMCPAll(k.Bot.GlobalMCPPath(), k.Bot.MCPPath())
	if err != nil {
		return "", err
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	for _, s := range servers {
		id := "mcp:" + s.Name
		if a, ok := link.LookupApp(s.Name); ok && a.Kind == link.AppServer {
			refs = append(refs, link.InstalledRef{ID: a.Title, Summary: a.Summary})
			continue
		}
		refs = append(refs, link.InstalledRef{ID: id, Summary: link.Blurb(id)})
	}
	return link.InstalledSection(refs), nil
}

func (k *Kernel) markLoaded(id string) {
	k.loadMu.Lock()
	defer k.loadMu.Unlock()
	if k.loaded == nil {
		k.loaded = map[string]bool{}
	}
	k.loaded[id] = true
}

func (k *Kernel) isLoaded(id string) bool {
	if k == nil {
		return false
	}
	k.loadMu.Lock()
	defer k.loadMu.Unlock()
	return k.loaded[id]
}

func (k *Kernel) clearLoaded() {
	if k == nil {
		return
	}
	k.loadMu.Lock()
	k.loaded = map[string]bool{}
	k.loadMu.Unlock()
}

// StageSecrets stores approval-card values for one catalog id until install reads them.
// The model never receives this map.
func (k *Kernel) StageSecrets(id string, values map[string]string) {
	if k == nil || id == "" || len(values) == 0 {
		return
	}
	cp := make(map[string]string, len(values))
	for name, value := range values {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		cp[name] = value
	}
	if len(cp) == 0 {
		return
	}
	k.secretMu.Lock()
	defer k.secretMu.Unlock()
	if k.secrets == nil {
		k.secrets = map[string]map[string]string{}
	}
	k.secrets[id] = cp
}

func (k *Kernel) takeSecrets(id string) map[string]string {
	if k == nil {
		return nil
	}
	k.secretMu.Lock()
	defer k.secretMu.Unlock()
	values := k.secrets[id]
	delete(k.secrets, id)
	return values
}

func (k *Kernel) secretValues() map[string]string {
	if k == nil {
		return nil
	}
	k.secretMu.Lock()
	out := map[string]string{}
	for _, m := range k.secrets {
		for name, value := range m {
			out[name] = value
		}
	}
	k.secretMu.Unlock()
	if k.Vault == nil {
		return out
	}
	// The backend redacts its own values; nothing is read back here, so this
	// works the same when the backend is a client to the vault daemon.
	for _, name := range k.Vault.Names() {
		out[name] = ""
	}
	return out
}

func scrubSecrets(s string, values map[string]string) string {
	if s == "" || len(values) == 0 {
		return s
	}
	forms := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		forms = append(forms, value)
	}
	sort.Slice(forms, func(i, j int) bool {
		if len(forms[i]) != len(forms[j]) {
			return len(forms[i]) > len(forms[j])
		}
		return forms[i] < forms[j]
	})
	pairs := make([]string, 0, 2*len(forms))
	for _, value := range forms {
		pairs = append(pairs, value, "[secret]")
	}
	// One pass, longest first at each position, as the vault does.
	return strings.NewReplacer(pairs...).Replace(s)
}

func linkOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (k *Kernel) stableLinkNames() []string {
	var names []string
	for _, name := range k.linkNames() {
		if lazyUntilLoad(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func (k *Kernel) redactor() func(string) string {
	base := bot.RedactText
	if k != nil && k.Redact != nil {
		base = k.Redact
	}
	return func(s string) string {
		return config.RedactHeld(base(s))
	}
}

func (k *Kernel) scrubEntry(m provider.Message) provider.Message {
	m = redactSkillRead(m)
	redact := k.redactor()
	values := k.secretValues()
	return m.MapText(func(text string) string {
		return scrubSecrets(redact(text), values)
	})
}

func lazyUntilLoad(name string) bool {
	if strings.HasPrefix(name, "mcp:") {
		return true
	}
	switch name {
	case link.NameWebhook, link.NameWatch, link.NameSkills, link.NameTelegram, link.NameLark, link.NameCoding, link.NameDesktop, link.NameBrowser:
		return true
	default:
		return false
	}
}

// silence unused import if tools is only used in toolsForLoaded
var _ tools.Tool = linksTool{}

// MCPStatus reads the matching mounted instance without mounting or connecting.
func (k *Kernel) MCPStatus(spec link.MCPServer) link.MCPStatus {
	k.linksMu.Lock()
	defer k.linksMu.Unlock()
	for _, l := range k.Links {
		if m, ok := l.(*link.MCP); ok && sameMCPSpec(m.Spec, spec) {
			return m.Status()
		}
	}
	return link.MCPStatus{State: "configured"}
}
