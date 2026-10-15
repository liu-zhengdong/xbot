// Package bot is the persistent entity. One bot has one name, one persona,
// one data directory, and one append-only transcript. There is no session.
package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/provider"
)

const DefaultPersona = "You are XBot, a personal assistant. Help the person with the tools granted to you. Be concise and concrete. Answer in the owner's language. Do not narrate a plan before you act."

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ErrNotFound means the bot directory has no config.
var ErrNotFound = errors.New("bot: not found")

// ErrExists means create was asked to replace a bot.
var ErrExists = errors.New("bot: already exists")

// Config is the bot's durable description. Links are granted here.
// Owners are person ids whose terminal input is a trusted instruction.
type Config struct {
	Name         string   `json:"name"`
	Persona      string   `json:"persona"`
	Links        []string `json:"links"`
	Owners       []string `json:"owners,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	ContextLimit int      `json:"context_limit,omitempty"`
	Webhooks     []string `json:"webhooks,omitempty"`
	Watches      []string `json:"watches,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	Description  string   `json:"description,omitempty"`
	Avatar       string   `json:"avatar,omitempty"`
	Style        *Style   `json:"style,omitempty"`
	// Proactivity is ignored. Old config files may still contain quiet, normal, or active.
	Proactivity    string    `json:"proactivity,omitempty"`
	QuietStart     string    `json:"quiet_start,omitempty"`
	QuietEnd       string    `json:"quiet_end,omitempty"`
	Timezone       string    `json:"timezone,omitempty"`
	Group          string    `json:"group,omitempty"`
	DisabledSkills []string  `json:"disabled_skills,omitempty"`
	Created        time.Time `json:"created"`
}

// SummaryBlock covers an inclusive sequence range of the append-only transcript.
// A later block with a wider range supersedes the blocks it contains.
type SummaryBlock struct {
	From  int       `json:"from"`
	To    int       `json:"to"`
	Text  string    `json:"text"`
	Focus string    `json:"focus,omitempty"`
	At    time.Time `json:"at,omitempty"`
}

// EnvRecord is the last environment snapshot. It is volatile context, not part of the stable prefix.
type EnvRecord struct {
	Hour        string    `json:"hour,omitempty"`
	Now         string    `json:"now,omitempty"`
	Sender      string    `json:"sender,omitempty"`
	Tools       []string  `json:"tools,omitempty"`
	Notes       int       `json:"notes,omitempty"`
	Persona     string    `json:"persona,omitempty"`
	Rules       string    `json:"rules,omitempty"`
	Memory      string    `json:"memory,omitempty"`
	Skills      string    `json:"skills,omitempty"`
	Profile     string    `json:"profile,omitempty"`
	OverlayKey  string    `json:"overlay_key,omitempty"`
	OverlayHash string    `json:"overlay_hash,omitempty"`
	OverlayBody string    `json:"overlay_body,omitempty"`
	At          time.Time `json:"at,omitempty"`
}

// SectionUsage is a rough token estimate for one wake, by context section.
type SectionUsage struct {
	Stable  int `json:"stable,omitempty"`
	Memory  int `json:"memory,omitempty"`
	Env     int `json:"env,omitempty"`
	History int `json:"history,omitempty"`
}

// Total sums the four sections.
func (u SectionUsage) Total() int { return u.Stable + u.Memory + u.Env + u.History }

// State holds compaction blocks and the last environment snapshot.
// Summary and Covered are the legacy single-summary cursor.
type State struct {
	Summary string         `json:"summary,omitempty"`
	Covered int            `json:"covered,omitempty"`
	Blocks  []SummaryBlock `json:"blocks,omitempty"`
	Env     EnvRecord      `json:"env,omitempty"`
	Usage   SectionUsage   `json:"usage,omitempty"`
}

// Entry is one transcript record.
// Blob, when set, names a file under transcript/blobs that holds the full tool
// output. Message.Content is then only a clipped preview.
// ReplyTo is the sequence of an earlier line this message quotes. Zero means none.
type Entry struct {
	Time    time.Time        `json:"time"`
	EventID string           `json:"event_id,omitempty"`
	Message provider.Message `json:"message"`
	Blob    string           `json:"blob,omitempty"`
	ReplyTo int              `json:"reply_to,omitempty"`
}

// Bot is one loaded bot directory.
type Bot struct {
	Root string
	cfg  Config
}

// Create makes ~/.xbot/bots/<name> with a config and an empty transcript.
func Create(root, name, persona string, links []string) (*Bot, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("bot: invalid name %q", name)
	}
	if persona == "" {
		persona = DefaultPersona
	}
	dir := Dir(root, name)
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return nil, fmt.Errorf("bot %q: %w", name, ErrExists)
	}
	cfg := Config{
		Name:    name,
		Persona: persona,
		Links:   append([]string(nil), links...),
		Created: time.Now().UTC(),
	}
	b := &Bot{Root: root, cfg: cfg}
	if err := b.SaveConfig(); err != nil {
		return nil, err
	}
	if err := b.SaveState(State{}); err != nil {
		return nil, err
	}
	if err := b.WritePersona(persona); err != nil {
		return nil, err
	}
	return b, nil
}

// Load reads an existing bot.
func Load(root, name string) (*Bot, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("bot: invalid name %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(Dir(root, name), "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("bot %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	return &Bot{Root: root, cfg: cfg}, nil
}

// List returns bots sorted by name.
func List(root string) ([]Config, error) {
	dir := filepath.Join(root, "bots")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Config
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		b, err := Load(root, ent.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, b.Config())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Dir is the bot's data directory.
func Dir(root, name string) string {
	return filepath.Join(root, "bots", name)
}

func (b *Bot) Name() string { return b.cfg.Name }

func (b *Bot) Config() Config { return b.cfg }

func (b *Bot) dir() string { return Dir(b.Root, b.cfg.Name) }

// SaveConfig writes config.json.
func (b *Bot) SaveConfig() error {
	raw, err := fsutil.Marshal(b.cfg)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(b.dir(), "config.json"), raw, 0600)
}

// SaveState writes the compaction cursor.
func (b *Bot) SaveState(st State) error {
	raw, err := fsutil.Marshal(st)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(b.dir(), "state.json"), raw, 0600)
}

// LoadState returns a zero state when the file is missing.
func (b *Bot) LoadState() (State, error) {
	raw, err := os.ReadFile(filepath.Join(b.dir(), "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, err
	}
	return st, nil
}

// Append adds one transcript record to the open tail segment and updates the index.
func (b *Bot) Append(e Entry) error {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	return b.store().append(e, true)
}

// Entries reads the transcript in order. Large tool outputs stay as previews;
// Hydrate loads the blob.
func (b *Bot) Entries() ([]Entry, error) {
	return b.store().entries()
}

// InboxPath is the bot's persisted inbox.
func (b *Bot) InboxPath() string {
	return filepath.Join(b.dir(), "inbox.jsonl")
}

// MCPPath is the bot's MCP server list. Secret values are not stored there.
func (b *Bot) MCPPath() string {
	return filepath.Join(b.dir(), "mcp.json")
}

// SecretPath is the bot's link secrets. Values stay out of the model prompt.
func (b *Bot) SecretPath() string {
	return filepath.Join(b.dir(), "secrets.json")
}

// SetBinding stores the provider and model names. The API key stays in the environment.
func (b *Bot) SetBinding(providerName, modelName string) error {
	b.cfg.Provider = providerName
	b.cfg.Model = modelName
	return b.SaveConfig()
}

// InheritBinding copies the provider name, model, and context limit.
// Webhooks, watches, and the transcript stay behind.
func (b *Bot) InheritBinding(from Config) error {
	b.cfg.Provider = from.Provider
	b.cfg.Model = from.Model
	b.cfg.ContextLimit = from.ContextLimit
	return b.SaveConfig()
}

// GrantLink adds a link name if it is not already granted.
func (b *Bot) GrantLink(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("bot: empty link")
	}
	next := addUnique(b.cfg.Links, name)
	if len(next) == len(b.cfg.Links) {
		return nil
	}
	b.cfg.Links = next
	return b.SaveConfig()
}

// DisableSkill adds slug to this bot's skill disable list.
func (b *Bot) DisableSkill(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return errors.New("bot: bad skill")
	}
	b.cfg.DisabledSkills = addUnique(b.cfg.DisabledSkills, slug)
	return b.SaveConfig()
}

// EnableSkill removes slug from this bot's skill disable list.
func (b *Bot) EnableSkill(slug string) error {
	slug = strings.TrimSpace(slug)
	next := make([]string, 0, len(b.cfg.DisabledSkills))
	for _, s := range b.cfg.DisabledSkills {
		if s == slug {
			continue
		}
		next = append(next, s)
	}
	b.cfg.DisabledSkills = next
	return b.SaveConfig()
}

// RevokeLink removes a link name. A name that is not granted is left as it is.
func (b *Bot) RevokeLink(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("bot: empty link")
	}
	next := make([]string, 0, len(b.cfg.Links))
	found := false
	for _, l := range b.cfg.Links {
		if l == name {
			found = true
			continue
		}
		next = append(next, l)
	}
	if !found {
		return nil
	}
	b.cfg.Links = next
	return b.SaveConfig()
}

// ValidName reports whether name can be a bot directory.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// SetGroup records the sidebar folder. An empty name clears it.
func (b *Bot) SetGroup(name string) error {
	b.cfg.Group = strings.TrimSpace(name)
	return b.SaveConfig()
}

// SetPinned records whether this bot sorts ahead of unpinned bots.
func (b *Bot) SetPinned(on bool) error {
	b.cfg.Pinned = on
	return b.SaveConfig()
}

// SetOwners records the person ids allowed to give trusted terminal instructions.
func (b *Bot) SetOwners(ids []string) error {
	b.cfg.Owners = append([]string(nil), ids...)
	return b.SaveConfig()
}

// PersonaPath is the editable persona file. It wins over config.json when present.
func (b *Bot) PersonaPath() string { return filepath.Join(b.dir(), "persona.md") }

// RulesPath is the editable rules file.
func (b *Bot) RulesPath() string { return filepath.Join(b.dir(), "rules.md") }

// RoutineDir holds one JSON file per routine.
func (b *Bot) RoutineDir() string { return filepath.Join(b.dir(), "routines") }

// AgentDir is one subagent's transcript directory.
func (b *Bot) AgentDir(id string) string { return filepath.Join(b.dir(), "agents", id) }

// OutboxPath is the notification log.
func (b *Bot) OutboxPath() string { return filepath.Join(b.dir(), "outbox.jsonl") }

// HeldPath stores notifications kept until quiet hours end.
func (b *Bot) HeldPath() string { return filepath.Join(b.dir(), "held.json") }

// ReadPersona returns persona.md, or the config string when the file is missing.
func (b *Bot) ReadPersona() (string, error) {
	return readText(b.PersonaPath(), b.cfg.Persona, DefaultPersona)
}

// ReadRules returns rules.md, or fallback when the file is missing.
func (b *Bot) ReadRules(fallback string) (string, error) {
	return readText(b.RulesPath(), fallback, fallback)
}

// WritePersona replaces persona.md. The transcript is not rewritten.
func (b *Bot) WritePersona(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("bot: empty persona")
	}
	if len(text) > 32<<10 {
		return errors.New("bot: persona too long")
	}
	return fsutil.WriteAtomic(b.PersonaPath(), []byte(text+"\n"), 0600)
}

// WriteRules replaces rules.md.
func (b *Bot) WriteRules(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("bot: empty rules")
	}
	if len(text) > 32<<10 {
		return errors.New("bot: rules too long")
	}
	return fsutil.WriteAtomic(b.RulesPath(), []byte(text+"\n"), 0600)
}

func readText(path, fallback, def string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if strings.TrimSpace(fallback) != "" {
			return strings.TrimSpace(fallback), nil
		}
		return strings.TrimSpace(def), nil
	}
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(string(raw)); s != "" {
		return s, nil
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback), nil
	}
	return strings.TrimSpace(def), nil
}

// LockHandle blocks until this process may run the bot's wake loop.
func (b *Bot) LockHandle() (*fsutil.FileLock, error) {
	return fsutil.Lock(filepath.Join(b.dir(), "handle.lock"))
}

// TryLockHandle reports whether another process already holds the wake lock.
func (b *Bot) TryLockHandle() (*fsutil.FileLock, bool, error) {
	return fsutil.TryLock(filepath.Join(b.dir(), "handle.lock"))
}

// Grants reports whether the bot config includes the link name.
func (b *Bot) Grants(name string) bool {
	for _, n := range b.cfg.Links {
		if n == name {
			return true
		}
	}
	return false
}

// WebhookDir holds one JSON file per webhook secret.
func (b *Bot) WebhookDir() string { return filepath.Join(b.dir(), "webhooks") }

// WatchDir holds watch globs and the poller's cursor.
func (b *Bot) WatchDir() string { return filepath.Join(b.dir(), "watches") }

// GrantWebhook records the hook name and the webhook link.
func (b *Bot) GrantWebhook(name string) error {
	b.cfg.Webhooks = addUnique(b.cfg.Webhooks, name)
	b.cfg.Links = addUnique(b.cfg.Links, "webhook")
	return b.SaveConfig()
}

// RevokeWebhook drops the hook name. The link grant stays.
func (b *Bot) RevokeWebhook(name string) error {
	b.cfg.Webhooks = dropName(b.cfg.Webhooks, name)
	return b.SaveConfig()
}

// GrantWatch records the watch id and the watch link.
func (b *Bot) GrantWatch(id string) error {
	b.cfg.Watches = addUnique(b.cfg.Watches, id)
	b.cfg.Links = addUnique(b.cfg.Links, "watch")
	return b.SaveConfig()
}

// RevokeWatch drops the watch id. The link grant stays.
func (b *Bot) RevokeWatch(id string) error {
	b.cfg.Watches = dropName(b.cfg.Watches, id)
	return b.SaveConfig()
}

func addUnique(in []string, name string) []string {
	for _, s := range in {
		if s == name {
			return in
		}
	}
	return append(in, name)
}

func dropName(in []string, name string) []string {
	var out []string
	for _, s := range in {
		if s != name {
			out = append(out, s)
		}
	}
	return out
}
