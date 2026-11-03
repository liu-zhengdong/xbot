package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/code"
	"xbot/internal/config"
	"xbot/internal/desktop"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/vault"
	"xbot/internal/work"
)

// linksTool carries the links domain helpers. It is no longer a model tool:
// search/load/install/remove all run through the current instance CLI
// ("$XBOT_BIN" links <action> --self), and install/remove reach these helpers
// through the runtime handler's owner-card path. Secret values arrive from
// the approval card, never from the model, and never go back out.
type linksTool struct{ k *Kernel }

// cardSummary builds the owner-card summary for install and remove, matching
// the retired tool Confirm byte for byte: install is "install <id>\n<summary>
// \nsecrets: <names>" (or connectorCard for a store connector, "install <id>"
// on a lookup miss) and remove is "remove <id>". The web secrets stage reads
// the id from the first line and the key names from the secrets line.
func cardSummary(action, id string) string {
	switch action {
	case "install":
		e, ok := link.Lookup(id)
		if !ok {
			if c, ok := storeConnector(id); ok {
				return connectorCard(c)
			}
			return "install " + id
		}
		var b strings.Builder
		fmt.Fprintf(&b, "install %s\n%s", e.ID, linkOneLine(e.Summary))
		if len(e.Secrets) > 0 {
			fmt.Fprintf(&b, "\nsecrets: %s", strings.Join(e.Secrets, ", "))
		}
		return b.String()
	case "remove":
		return "remove " + id
	default:
		return ""
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
	servers, _ := t.k.mcpSpecs()
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
				s.Secret = link.GlobalSecretLookup(t.k.Bot.Root)
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
	return scrubSecrets("installed "+c.ID, values), nil
}

// removeConnector takes a store server out of the global and this Bot's
// mcp.json and drops the keys this Bot stored for it. Removing twice is fine.
func (t linksTool) removeConnector(c link.CatalogItem) (string, error) {
	for _, path := range []string{t.k.Bot.GlobalMCPPath(), t.k.Bot.MCPPath()} {
		if err := mcp.RemoveMCP(path, c.ID, mcp.Options{IsProviderKey: config.ProviderKey}); err != nil && !errors.Is(err, mcp.ErrMCPMissing) {
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
		return "", fmt.Errorf("links: MCP tools use the CLI: \"$XBOT_BIN\" mcp tools %q --self --json; use mcp describe/call with the remote tool name", name)
	}

	// An organ is on from boot; loading it only lists its tools.
	if o, ok := link.LookupOrgan(id); ok {
		return t.loadedNames(o.Grant)
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
	var out []tools.Tool
	for _, l := range links {
		if l == nil {
			continue
		}
		if l.Name() == id || (id != "" && l.Name() == grantName(id)) {
			out = append(out, l.Tools()...)
		}
	}
	return out, nil
}

// snapshotLinks mounts catalog grants that were installed after this kernel
// started, and drops catalog grants that were removed. Runner links stay.
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
	// Organs are on from boot: no grant mounts or drops them.
	for _, o := range link.Organs() {
		if have[o.Grant] {
			continue
		}
		if mounted := k.mountGrantLocked(o.Grant); mounted != nil {
			kept = append(kept, mounted)
			have[o.Grant] = true
		}
	}
	k.Links = kept
}

// MountOrgans assembles the same always-on capabilities used by the registry.
// The caller opens the workspace first and can pass already mounted external links.
func MountOrgans(b *bot.Bot, links []link.Link) []link.Link {
	k := &Kernel{Bot: b, Links: links}
	return k.snapshotLinks()
}

// mcpSpecs reads the effective runtime inputs retained by the Manager.
func (k *Kernel) mcpSpecs() ([]mcp.Spec, error) {
	if k.Resources != nil {
		return k.Resources.MCP.Specs(), nil
	}
	return mcp.LoadMCPAll(mcp.Options{IsProviderKey: config.ProviderKey}, k.Bot.GlobalMCPPath(), k.Bot.MCPPath())
}

func catalogRuntime(name string) bool {
	return link.StoreGrant(name)
}

// storeSecrets uses the same global backend for connector and MCP keys.
func (k *Kernel) storeSecrets(values map[string]string) error {
	backend, err := vault.OpenGlobal(k.Bot.Root)
	if err != nil {
		return err
	}
	err = link.SetProductSecrets(backend, values)
	if local, ok := backend.(*vault.Store); ok && err == nil {
		vault.Register(local)
	}
	return err
}

// dropSecrets removes shared link credentials.
func (k *Kernel) dropSecrets(e link.CatalogEntry) error {
	backend, err := vault.OpenGlobal(k.Bot.Root)
	if err != nil {
		return err
	}
	return link.RemoveProductSecrets(backend, e.Secrets)
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
	dir := bot.Dir(k.Bot.Root, k.currentName())
	switch name {
	case link.NameTerminal:
		return link.Terminal{}
	case link.NameTools:
		return link.NewToolsLinkAt(work.BotDir(k.Bot.Root, k.currentName()), work.SharedDir(k.Bot.Root))
	case link.NameTimer:
		sender := "timer"
		if owners := k.Bot.Config().Owners; len(owners) > 0 {
			sender = owners[0]
		}
		timer := &link.Timer{Dir: k.Bot.RoutineDir(), Sender: sender}
		k.bindCurrentPaths(timer)
		return timer
	case link.NameMemory:
		return link.Memory{}
	case link.NamePersona:
		return link.Persona{}
	case link.NameContext:
		return link.Context{}
	case link.NameAgents:
		return link.Agents{}
	case link.NameWeb:
		return link.Web{}
	case link.NamePeers:
		return link.Peers{}
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
		k.bindCurrentPaths(l)
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
			l.UseTranscript(func(text string) error {
				return k.appendCurrentEntry(bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
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
		a, ok := link.LookupApp(e.ID)
		if !ok {
			a, ok = link.LookupOrgan(e.ID)
		}
		if ok {
			refs = append(refs, link.InstalledRef{ID: a.Title, Summary: a.Summary})
		}
	}
	servers, err := k.mcpSpecs()
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
		s = config.RedactHeld(base(s))
		if k != nil && k.Resources != nil {
			s = k.Resources.Redact(s)
		}
		return s
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
	return link.StoreGrant(name)
}
