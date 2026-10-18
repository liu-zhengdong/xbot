package kernel

import (
	"context"
	"encoding/json"
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
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// linksTool lets the bot search, install, remove, and load links.
// Install always asks the owner. Secret values arrive from the approval card,
// never from the model, and never go back out in the tool result.
type linksTool struct{ k *Kernel }

func (linksTool) Name() string { return "links" }
func (linksTool) Description() string {
	return "Search the link catalog, install or remove one, or load its tool schemas for this turn only. install and remove both ask the owner. When the catalog entry lists secrets, the owner fills the values in the approval card; you never see those values. Installed links are one line each in the system prompt. load brings that link's tool schemas into later steps of this turn, then they drop off. search returns summaries, including when the prompt lists ids only."
}
func (linksTool) Parameters() any {
	return schema(map[string]any{
		"action": stringProp("search, install, remove, or load"),
		"id":     stringProp("catalog id, or mcp:<server> for load"),
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

func (t linksTool) Run(_ context.Context, args json.RawMessage) (string, error) {
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
		return t.load(strings.TrimSpace(in.ID))
	default:
		return "", fmt.Errorf("links: action must be search, install, remove, or load")
	}
}

func (t linksTool) search(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	granted := map[string]bool{}
	for _, name := range t.k.Bot.Config().Links {
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
	if servers, err := link.LoadMCP(t.k.Bot.MCPPath()); err == nil {
		for _, s := range servers {
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

func (t linksTool) remove(id string) (string, error) {
	e, ok := link.Lookup(id)
	if !ok {
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

func (t linksTool) load(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("links: id is required")
	}
	if strings.HasPrefix(id, "mcp:") {
		name := strings.TrimPrefix(id, "mcp:")
		servers, err := link.LoadMCP(t.k.Bot.MCPPath())
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
		t.k.markLoaded(id)
		return t.loadedNames(id)
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
	list := t.k.toolsForLoaded(id)
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
	return "loaded " + id + ": " + strings.Join(names, ", "), nil
}

func (k *Kernel) toolsForLoaded(id string) []tools.Tool {
	links := k.snapshotLinks()
	if id == link.NameSkills || id == "skills" {
		if namedLink(links, link.NameSkills) {
			return []tools.Tool{skillTool{k}, writeSkillTool{k}}
		}
		return nil
	}
	var out []tools.Tool
	for _, l := range links {
		if l == nil {
			continue
		}
		if l.Name() == id || (id != "" && l.Name() == grantName(id)) {
			out = append(out, l.Tools()...)
		}
	}
	return out
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
	for _, name := range k.Bot.Config().Links {
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
	for _, name := range k.Bot.Config().Links {
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
	k.Links = kept
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
			l.UseProvider(k.Provider)
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
				return b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: text}})
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
	for _, name := range k.Bot.Config().Links {
		granted[name] = true
	}
	var refs []link.InstalledRef
	for _, e := range link.Catalog() {
		if e.Grant == "" || !granted[e.Grant] {
			continue
		}
		refs = append(refs, link.InstalledRef{ID: e.ID, Summary: e.Summary})
	}
	servers, err := link.LoadMCP(k.Bot.MCPPath())
	if err != nil {
		return "", err
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	for _, s := range servers {
		id := "mcp:" + s.Name
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
	for _, value := range forms {
		s = strings.ReplaceAll(s, value, "[secret]")
	}
	return s
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
	m.Content = redact(m.Content)
	if len(m.ToolCalls) > 0 {
		calls := append([]provider.ToolCall(nil), m.ToolCalls...)
		for i := range calls {
			calls[i].Function.Arguments = redact(calls[i].Function.Arguments)
		}
		m.ToolCalls = calls
	}
	values := k.secretValues()
	if len(values) == 0 {
		return m
	}
	m.Content = scrubSecrets(m.Content, values)
	if len(m.ToolCalls) == 0 {
		return m
	}
	calls := append([]provider.ToolCall(nil), m.ToolCalls...)
	for i := range calls {
		calls[i].Function.Arguments = scrubSecrets(calls[i].Function.Arguments, values)
	}
	m.ToolCalls = calls
	return m
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
