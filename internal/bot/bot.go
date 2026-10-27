// Package bot is the persistent entity. One bot has one name, one persona,
// one data directory, and one append-only transcript. There is no session.
package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xbot/internal/config"
	"xbot/internal/fsutil"
	"xbot/internal/provider"
)

const DefaultPersona = "You are XBot, a personal assistant. Help the person with the tools granted to you. Be concise and concrete. Answer in the owner's language. Do not narrate a plan before you act."

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
	AvatarSeed   string   `json:"avatar_seed,omitempty"`
	// PersonaCard is the id of the owner's persona card this bot speaks from,
	// stacked after its duty (persona.md). Empty uses the owner's default card.
	PersonaCard string `json:"persona_card,omitempty"`
	// Proactivity is ignored. Old config files may still contain quiet, normal, or active.
	Proactivity string `json:"proactivity,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	// Role picks the working defaults: "chat" (default) for interactive use and
	// "dev" for long engineering runs. It sets the background budget unless
	// that is set explicitly.
	Role string `json:"role,omitempty"`
	// BackgroundMaxTime overrides the role's background budget in seconds. Zero
	// uses the role default; negative lets a background run with no time limit.
	BackgroundMaxTime int       `json:"background_max_time,omitempty"`
	Group             string    `json:"group,omitempty"`
	DisabledSkills    []string  `json:"disabled_skills,omitempty"`
	Created           time.Time `json:"created"`
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

// TaskEnv is a sampled executor task, published in the conversation environment.
type TaskEnv struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Status   string `json:"status"`
	Position int    `json:"position,omitempty"`
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

// State holds compaction blocks and the fixed memory prefix.
// Summary and Covered are the legacy single-summary cursor.
type State struct {
	Summary string         `json:"summary,omitempty"`
	Covered int            `json:"covered,omitempty"`
	Blocks  []SummaryBlock `json:"blocks,omitempty"`
	Usage   SectionUsage   `json:"usage,omitempty"`
	// BoundModel is provider/model at the last wake. A change with an open
	// tool call is a model switch: the kernel closes the call with a tombstone
	// instead of an interrupt. Empty on older state files.
	BoundModel string `json:"bound_model,omitempty"`
	// MemRender identifies the rendering format of all memory views.
	MemRender int `json:"mem_render,omitempty"`
	// MemSnap is the memory text injected after the system prompt; frozen
	// between compactions so the prefix cache keeps hitting.
	MemSnap string `json:"mem_snap,omitempty"`
	// MemSnapSet distinguishes an empty snapshot from no snapshot yet.
	MemSnapSet bool `json:"mem_snap_set,omitempty"`
}

// Fork marker kinds and end statuses.
const (
	ForkKindStart = "fork"
	ForkKindEnd   = "fork_end"

	ForkDone        = "done"
	ForkFailed      = "failed"
	ForkCancelled   = "cancelled"
	ForkInterrupted = "interrupted"
)

// ForkMark is a branch marker row. Kind is "fork" for the line that opens a
// branch and "fork_end" for the line that closes it. From and Task belong to
// the opening line only; Status and Steps belong to the closing line only.
type ForkMark struct {
	Kind   string `json:"kind"`
	Branch string `json:"branch"`
	From   int    `json:"from,omitempty"`
	Task   string `json:"task,omitempty"`
	Status string `json:"status,omitempty"`
	Steps  int    `json:"steps,omitempty"`
}

// Origin says who wrote a transcript line. It is written by the author, not
// guessed from the text, so a reader never has to recognize a note by its
// prefix: Internal lines are kernel notes the model must see and the owner must
// not. Every new write sets one of these; only rows from before the field
// existed carry none.
const (
	// OriginOwner is a line shown to the owner: a message they typed, an event
	// the kernel turned into a line for them to read, an approval card, or a
	// coding-agent card.
	OriginOwner = "owner"
	// OriginInternal is a line the kernel wrote for the model: environment
	// updates, reminders, completion gates, stop notes. The interface hides it.
	OriginInternal = "internal"
)

// Entry is one transcript record.
// Blob, when set, names a file under transcript/blobs that holds the full tool
// output. Message.Content is then only a clipped preview.
// ReplyTo is the sequence of an earlier line this message quotes. Zero means none.
// Branch names the background task this line belongs to; empty means the main line.
// Mark, when set, makes this a branch marker row instead of a message.
// Images names pictures the owner attached to this line, as file names in the
// bot workspace (see internal/web pasted.go). The bytes stay out of the line: the
// file sits in the workspace, so view_image opens it and the transcript stays small.
type Entry struct {
	Time    time.Time        `json:"time"`
	EventID string           `json:"event_id,omitempty"`
	Message provider.Message `json:"message"`
	Blob    string           `json:"blob,omitempty"`
	ReplyTo int              `json:"reply_to,omitempty"`
	Branch  string           `json:"branch,omitempty"`
	Origin  string           `json:"origin,omitempty"`
	Mark    *ForkMark        `json:"mark,omitempty"`
	Images  []string         `json:"images,omitempty"`
}

// IsInternal reports a kernel-written line the owner did not say.
//
// A row written by the current kernel says so in Origin, and that answer wins:
// the owner is free to type anything, including text that opens like a tag.
// Rows written before the field existed carry no origin at all, and only those
// are recognized by their opening tag. Delete the prefix list once no
// transcript predates the field.
func (e Entry) IsInternal() bool {
	switch e.Origin {
	case OriginInternal:
		return true
	case OriginOwner:
		return false
	}
	if e.Message.Role != provider.RoleUser {
		return false
	}
	return hasInternalPrefix(e.Message.Text())
}

// internalPrefixes are the openings of kernel notes written before the origin
// field. They are data, not instructions: each one is a tag the kernel emits.
var internalPrefixes = []string{
	"<environment_", "<memory>", "<summary ", "<background_tasks>",
	"<todo_gate>", "<stop_reason>",
	"[继续]", "[主人的补充] ", "[排队] ", "[打断] ",
}

func hasInternalPrefix(content string) bool {
	c := strings.TrimSpace(content)
	for _, p := range internalPrefixes {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
}

// Bot is one loaded bot directory.
type Bot struct {
	Root string
	cfg  Config
	// cardSet means SetPersonaCard changed the binding. Other saves keep the
	// binding on disk, so a bot loaded earlier cannot undo a newer pick.
	cardSet   bool
	skillsSet bool
}

// Create makes ~/.xbot/bots/<name> with a config and an empty transcript.
func Create(root, name, persona string, links []string) (*Bot, error) {
	if !ValidName(name) || !fsutil.PortableName(name) {
		return nil, fmt.Errorf("bot: invalid name %q", name)
	}
	if persona == "" {
		persona = DefaultPersona
	}
	dir := Dir(root, name)
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return nil, fmt.Errorf("bot %q: %w", name, ErrExists)
	}
	def, err := config.EnsureDefault(root)
	if err != nil {
		return nil, err
	}
	cfg := Config{
		Name:       name,
		Persona:    persona,
		Provider:   def.Name,
		Model:      def.Models[0].Name,
		Links:      append([]string(nil), links...),
		Avatar:     AvatarGenerated,
		AvatarSeed: name,
		Created:    time.Now().UTC(),
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
	if !ValidName(name) {
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
	b := &Bot{Root: root, cfg: cfg}
	if err := b.migrateStyle(raw); err != nil {
		return nil, err
	}
	return b, nil
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
			log.Printf("bot: skip %q: %v", ent.Name(), err)
			continue
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
	return fsutil.WithLock(filepath.Join(b.dir(), "config.json.lock"), b.saveConfig)
}

// saveConfig requires the config lock, shared with legacy style migration.
func (b *Bot) saveConfig() error {
	if !b.skillsSet {
		b.cfg.DisabledSkills = b.DisabledSkills()
	}
	if !b.cardSet {
		b.cfg.PersonaCard = b.boundCard()
	}
	raw, err := fsutil.Marshal(b.cfg)
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(b.dir(), "config.json"), raw, 0600); err != nil {
		return err
	}
	b.cardSet = false
	b.skillsSet = false
	return nil
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
	_, err := b.AppendSeq(e)
	return err
}

// AppendSeq adds one transcript record and returns the sequence it landed on.
// The sequence is the line just written, not a later count of the file.
func (b *Bot) AppendSeq(e Entry) (int, error) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	return b.store().appendSeq(e, true)
}

// AppendFork opens a background branch and returns the marker's sequence.
// branch must be a bg_ task id and from must name an existing line.
func (b *Bot) AppendFork(branch string, from int, task string) (int, error) {
	if !validBranch(branch) {
		return 0, fmt.Errorf("bot: invalid fork branch %q", branch)
	}
	if from < 1 {
		return 0, fmt.Errorf("bot: fork from %d", from)
	}
	n, err := b.Count()
	if err != nil {
		return 0, err
	}
	if from >= n+1 {
		return 0, fmt.Errorf("bot: fork from %d is not an existing line (next seq %d)", from, n+1)
	}
	return b.appendSeq(Entry{
		Message: provider.Message{Role: provider.RoleUser},
		Mark:    &ForkMark{Kind: ForkKindStart, Branch: branch, From: from, Task: task},
	})
}

// AppendForkEnd closes a background branch and returns the marker's sequence.
func (b *Bot) AppendForkEnd(branch, status string, steps int) (int, error) {
	if !validBranch(branch) {
		return 0, fmt.Errorf("bot: invalid fork branch %q", branch)
	}
	if !validForkStatus(status) {
		return 0, fmt.Errorf("bot: invalid fork status %q", status)
	}
	if steps < 0 {
		return 0, fmt.Errorf("bot: invalid fork steps %d", steps)
	}
	return b.appendSeq(Entry{
		Message: provider.Message{Role: provider.RoleUser},
		Mark:    &ForkMark{Kind: ForkKindEnd, Branch: branch, Status: status, Steps: steps},
	})
}

func (b *Bot) appendSeq(e Entry) (int, error) {
	return b.AppendSeq(e)
}

func validBranch(branch string) bool {
	return strings.HasPrefix(branch, "bg_") && len(branch) > len("bg_")
}

func validForkStatus(status string) bool {
	switch status {
	case ForkDone, ForkFailed, ForkCancelled, ForkInterrupted:
		return true
	}
	return false
}

// IsForkMark reports a branch marker line. Markers stay out of model context,
// compaction, and memory extraction.
func (e Entry) IsForkMark() bool { return e.Mark != nil }

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

// VaultDir is the bot's encrypted credential vault. Mode 0700; files 0600.
func (b *Bot) VaultDir() string {
	return filepath.Join(b.dir(), "vault")
}

// keysSubdir is the data-root directory holding per-bot vault keys. It sits
// outside bots/<name>/, so a bot's own shell or file tools cannot reach the
// key next to secrets.enc.
const keysSubdir = "keys/vault"

// VaultKeysDir is the directory holding every bot's vault key: <root>/keys/vault.
func VaultKeysDir(root string) string {
	return filepath.Join(root, keysSubdir)
}

// VaultKeyPath is this bot's vault key file: <root>/keys/vault/<name>.key.
// It lives outside the bot workspace on purpose; see docs/atlas/security.md.
func (b *Bot) VaultKeyPath() string {
	return filepath.Join(VaultKeysDir(b.Root), b.cfg.Name+".key")
}

// SetBinding stores the provider and model names. The API key stays in the environment.
func (b *Bot) SetBinding(providerName, modelName string) error {
	b.cfg.Provider = providerName
	b.cfg.Model = modelName
	return b.SaveConfig()
}

// BindLegacy moves bots saved without a provider onto the deepseek entry,
// keeping their model name (empty becomes XBOT_MODEL or deepseek-flash, the
// model the environment used to give them) and adding it to the entry.
func BindLegacy(root string) error {
	cfgs, err := List(root)
	if err != nil {
		return err
	}
	for _, c := range cfgs {
		if strings.TrimSpace(c.Provider) != "" {
			continue
		}
		model := strings.TrimSpace(c.Model)
		if model == "" {
			model = config.FromEnv(config.Getenv).Model
		}
		if _, err := config.EnsureDefault(root, model); err != nil {
			return err
		}
		b, err := Load(root, c.Name)
		if err != nil {
			return err
		}
		if err := b.SetBinding(config.DefaultProvider, model); err != nil {
			return err
		}
	}
	return nil
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
	return b.setSkillDisabled(slug, true)
}

// EnableSkill removes slug from this bot's skill disable list.
func (b *Bot) EnableSkill(slug string) error {
	return b.setSkillDisabled(slug, false)
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

// ValidName reports whether name can be a bot directory. Besides the
// path-safety regex and the byte cap, it rejects code points that render as
// nothing; otherwise a bot could be named to look exactly like another one.
func ValidName(name string) bool { return fsutil.ValidName(name) }

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

// AgentTypesDir holds the bot's own subagent type files (ADR 0016).
func (b *Bot) AgentTypesDir() string { return filepath.Join(b.dir(), "agents", "types") }

// OutboxPath is the notification log.
func (b *Bot) OutboxPath() string { return filepath.Join(b.dir(), "outbox.jsonl") }

// LegacyHeldPath is where quiet hours used to keep notes. It is only read to deliver what was left there.
func (b *Bot) LegacyHeldPath() string { return filepath.Join(b.dir(), "held.json") }

// ReadPersona returns persona.md, or the config string when the file is missing.
func (b *Bot) ReadPersona() (string, error) {
	return readText(b.PersonaPath(), b.cfg.Persona, DefaultPersona)
}

// ReadRules returns rules.md, or fallback when the file is missing.
func (b *Bot) ReadRules(fallback string) (string, error) {
	return readText(b.RulesPath(), fallback, fallback)
}

// MigrateRules serializes normalization with other rules writers. Missing files stay missing.
func (b *Bot) MigrateRules(normalize func(string) string) (string, error) {
	var result string
	err := fsutil.WithLock(b.RulesPath()+".lock", func() error {
		raw, err := os.ReadFile(b.RulesPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		result = normalize(string(raw))
		if result == strings.TrimSpace(string(raw)) {
			return nil
		}
		return fsutil.WriteAtomic(b.RulesPath(), []byte(result+"\n"), 0600)
	})
	return result, err
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
	if len(text) > 32<<10 {
		return errors.New("bot: rules too long")
	}
	return fsutil.WithLock(b.RulesPath()+".lock", func() error {
		return fsutil.WriteAtomic(b.RulesPath(), []byte(text+"\n"), 0600)
	})
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

// Grants reports whether the link is granted to this bot or to every bot.
func (b *Bot) Grants(name string) bool {
	for _, n := range b.EffectiveLinks() {
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
