package bot

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/person"
	"xbot/internal/work"
)

// Style is how the bot speaks. Empty fields are left out of the prompt.
type Style struct {
	Tone     string `json:"tone,omitempty"`
	Length   string `json:"length,omitempty"`
	Emoji    string `json:"emoji,omitempty"`
	Language string `json:"language,omitempty"`
}

const (
	// AvatarCustom means bots/<name>/avatar holds an uploaded image.
	AvatarCustom = "custom"
	maxAvatar    = 1 << 20
)

// GenerateAvatar is the seam for a future portrait generator.
// Nil means an unset avatar is a low-saturation color block plus an initial.
// Tests should set it through SetGenerateAvatar.
var GenerateAvatar func(name, description string) ([]byte, error)

var genMu sync.Mutex

// SetGenerateAvatar installs fn and returns a restore function.
func SetGenerateAvatar(fn func(name, description string) ([]byte, error)) func() {
	genMu.Lock()
	prev := GenerateAvatar
	GenerateAvatar = fn
	genMu.Unlock()
	return func() {
		genMu.Lock()
		GenerateAvatar = prev
		genMu.Unlock()
	}
}

// HasAvatarGenerator reports whether a portrait generator is installed.
func HasAvatarGenerator() bool {
	genMu.Lock()
	defer genMu.Unlock()
	return GenerateAvatar != nil
}

func callGenerator(name, description string) ([]byte, error) {
	genMu.Lock()
	fn := GenerateAvatar
	genMu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn(name, description)
}

// ProfileUpdate changes only the fields whose pointers are set.
// An empty string clears that field.
type ProfileUpdate struct {
	Description *string
	Tone        *string
	Length      *string
	Emoji       *string
	Language    *string
	Proactivity *string
	QuietStart  *string
	QuietEnd    *string
	Timezone    *string
}

// ApplyProfile validates and stores a partial profile update.
func (b *Bot) ApplyProfile(u ProfileUpdate) error {
	cfg := b.cfg
	if u.Description != nil {
		d, err := limitText(*u.Description, 160)
		if err != nil {
			return fmt.Errorf("bot: description: %w", err)
		}
		cfg.Description = d
	}
	st := Style{}
	if cfg.Style != nil {
		st = *cfg.Style
	}
	if u.Tone != nil {
		t, err := limitText(*u.Tone, 80)
		if err != nil {
			return fmt.Errorf("bot: tone: %w", err)
		}
		st.Tone = t
	}
	if u.Length != nil {
		v, err := oneOf(flat(*u.Length), "", "short", "medium", "long")
		if err != nil {
			return fmt.Errorf("bot: length: %w", err)
		}
		st.Length = v
	}
	if u.Emoji != nil {
		v, err := oneOf(flat(*u.Emoji), "", "none", "spare", "often")
		if err != nil {
			return fmt.Errorf("bot: emoji: %w", err)
		}
		st.Emoji = v
	}
	if u.Language != nil {
		v, err := limitText(*u.Language, 40)
		if err != nil {
			return fmt.Errorf("bot: language: %w", err)
		}
		st.Language = v
	}
	if st == (Style{}) {
		cfg.Style = nil
	} else {
		cfg.Style = &st
	}
	if u.Proactivity != nil {
		v, err := oneOf(flat(*u.Proactivity), "", "quiet", "normal", "active")
		if err != nil {
			return fmt.Errorf("bot: proactivity: %w", err)
		}
		cfg.Proactivity = v
	}
	start, end := cfg.QuietStart, cfg.QuietEnd
	if u.QuietStart != nil {
		start = flat(*u.QuietStart)
	}
	if u.QuietEnd != nil {
		end = flat(*u.QuietEnd)
	}
	if err := validateQuiet(start, end); err != nil {
		return err
	}
	cfg.QuietStart, cfg.QuietEnd = start, end
	if u.Timezone != nil {
		v := flat(*u.Timezone)
		if v != "" {
			if _, err := time.LoadLocation(v); err != nil {
				return fmt.Errorf("bot: timezone: %w", err)
			}
		}
		cfg.Timezone = v
	}
	b.cfg = cfg
	return b.SaveConfig()
}

// StyleCopy returns the speaking style, or a zero style when unset.
func (b *Bot) StyleCopy() Style {
	if b.cfg.Style == nil {
		return Style{}
	}
	return *b.cfg.Style
}

// ProfileText is the speaking-style block appended to the persona.
// An empty profile returns an empty string so the stable prefix stays unchanged.
func (b *Bot) ProfileText() string {
	var lines []string
	if d := flat(b.cfg.Description); d != "" {
		lines = append(lines, "About: "+d)
	}
	st := b.StyleCopy()
	if st.Tone != "" {
		lines = append(lines, "Tone: "+st.Tone)
	}
	switch st.Length {
	case "short":
		lines = append(lines, "Reply length: short. A few sentences at most.")
	case "medium":
		lines = append(lines, "Reply length: medium. A short paragraph unless the owner asks for more.")
	case "long":
		lines = append(lines, "Reply length: long. Explain fully when the topic needs it.")
	}
	switch st.Emoji {
	case "none":
		lines = append(lines, "Emoji: do not use emoji.")
	case "spare":
		lines = append(lines, "Emoji: use emoji sparingly.")
	case "often":
		lines = append(lines, "Emoji: emoji are welcome when they fit.")
	}
	if st.Language != "" {
		lines = append(lines, "Language: reply in "+st.Language+".")
	}
	switch b.cfg.Proactivity {
	case "quiet":
		lines = append(lines, "Proactivity: quiet. Owner-requested routines always notify. On a self-initiated or untrusted wake, notify the owner only when they would want to be interrupted. Set importance to high, otherwise stay silent.")
	case "normal":
		lines = append(lines, "Proactivity: normal. Owner-requested routines always notify. On a self-initiated or untrusted wake, notify the owner when something is worth their attention.")
	case "active":
		lines = append(lines, "Proactivity: active. Notify the owner about anything they might want to know, including on untrusted wakes. Importance may be low.")
	}
	if b.cfg.QuietStart != "" && b.cfg.QuietEnd != "" && b.cfg.QuietStart != b.cfg.QuietEnd {
		lines = append(lines, fmt.Sprintf("Quiet hours: %s–%s %s. Notifications are held and sent as one batch when quiet hours end.", b.cfg.QuietStart, b.cfg.QuietEnd, b.QuietLocation().String()))
	}
	return strings.Join(lines, "\n")
}

// ComposedPersona is persona.md plus the profile block when one is set.
func (b *Bot) ComposedPersona() (string, error) {
	persona, err := b.ReadPersona()
	if err != nil {
		return "", err
	}
	extra := b.ProfileText()
	if extra == "" {
		return persona, nil
	}
	return persona + "\n\n" + extra, nil
}

// AllowsNotify reports whether a routine wake at this proactivity should deliver
// a note of the given importance. Empty proactivity is normal. Empty importance is normal.
// Non-routine wakes do not use this gate.
func AllowsNotify(level, importance string) bool {
	level = flat(level)
	if level == "" {
		level = "normal"
	}
	importance = flat(importance)
	if importance == "" {
		importance = "normal"
	}
	rank := map[string]int{"low": 1, "normal": 2, "high": 3}
	need := map[string]int{"quiet": 3, "normal": 2, "active": 1}
	ir, ok := rank[importance]
	if !ok {
		ir = 2
	}
	nr, ok := need[level]
	if !ok {
		nr = 2
	}
	return ir >= nr
}

// QuietLocation is the clock for quiet hours: the bot's IANA zone,
// then the sole owner's zone, then the process local zone.
func (b *Bot) QuietLocation() *time.Location {
	if b != nil {
		if loc, ok := loadZone(b.cfg.Timezone); ok {
			return loc
		}
		if loc, ok := loadZone(b.ownerTimezone()); ok {
			return loc
		}
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

func (b *Bot) ownerTimezone() string {
	if b == nil || b.Root == "" {
		return ""
	}
	owners := b.cfg.Owners
	if len(owners) != 1 {
		return ""
	}
	p, err := person.Load(b.Root, owners[0])
	if err != nil {
		return ""
	}
	return p.Timezone
}

func loadZone(name string) (*time.Location, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	return loc, true
}

// InQuietAt reports whether now falls in this bot's quiet window.
func (b *Bot) InQuietAt(now time.Time) bool {
	if b == nil {
		return false
	}
	return InQuiet(b.cfg.QuietStart, b.cfg.QuietEnd, now.In(b.QuietLocation()))
}

// InQuiet reports whether now falls in [start, end) on now's own clock.
// An empty window, or equal endpoints, is off. A window may cross midnight.
func InQuiet(start, end string, now time.Time) bool {
	if start == "" || end == "" || start == end {
		return false
	}
	sm, ok1 := parseHHMM(start)
	em, ok2 := parseHHMM(end)
	if !ok1 || !ok2 || sm == em {
		return false
	}
	loc := now.Location()
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	cur := now.Hour()*60 + now.Minute()
	if sm < em {
		return cur >= sm && cur < em
	}
	return cur >= sm || cur < em
}

// AvatarColors is the fallback mark: a low-saturation block and an initial.
func AvatarColors(name string) (bg, ink, initial string) {
	initial = "?"
	for _, r := range name {
		initial = string(unicode.ToUpper(r))
		break
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	hue := float64(h.Sum32() % 360)
	return hslHex(hue, 0.30, 0.78), hslHex(hue, 0.30, 0.28), initial
}

// AvatarPath is the uploaded image. The MIME type lives beside it.
func (b *Bot) AvatarPath() string { return filepath.Join(b.dir(), "avatar") }

func (b *Bot) avatarMIMEPath() string { return filepath.Join(b.dir(), "avatar.mime") }

// SetAvatar stores an uploaded image and marks the avatar custom.
func (b *Bot) SetAvatar(data []byte, mime string) error {
	kind := SniffImage(data)
	if kind == "" {
		return errors.New("bot: unsupported avatar type")
	}
	if len(data) > maxAvatar {
		return errors.New("bot: avatar is too large")
	}
	if err := fsutil.WriteAtomic(b.AvatarPath(), data, 0600); err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(b.avatarMIMEPath(), []byte(kind+"\n"), 0600); err != nil {
		return err
	}
	b.cfg.Avatar = AvatarCustom
	return b.SaveConfig()
}

// ClearAvatar removes an upload. The color block is used again.
func (b *Bot) ClearAvatar() error {
	_ = os.Remove(b.AvatarPath())
	_ = os.Remove(b.avatarMIMEPath())
	b.cfg.Avatar = ""
	return b.SaveConfig()
}

// AvatarBytes returns a custom image, or a generated portrait when the seam is set.
// ok is false when the caller should draw the color block.
func (b *Bot) AvatarBytes() (data []byte, mime string, ok bool, err error) {
	if b.cfg.Avatar == AvatarCustom {
		data, err = os.ReadFile(b.AvatarPath())
		if err != nil {
			return nil, "", false, err
		}
		mime = "application/octet-stream"
		if raw, rerr := os.ReadFile(b.avatarMIMEPath()); rerr == nil {
			if s := flat(string(raw)); s != "" {
				mime = s
			}
		}
		if kind := SniffImage(data); kind != "" {
			mime = kind
		}
		return data, mime, true, nil
	}
	data, err = callGenerator(b.Name(), b.cfg.Description)
	if err != nil || len(data) == 0 {
		return nil, "", false, err
	}
	return data, "image/png", true, nil
}

// SniffImage returns a supported image MIME type, or empty.
func SniffImage(data []byte) string {
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == '\r' && data[5] == '\n' && data[6] == 0x1a && data[7] == '\n' {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

// TemplateRoutine is one routine inside an exported template.
type TemplateRoutine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Prompt  string `json:"prompt"`
	Cron    string `json:"cron,omitempty"`
	At      string `json:"at,omitempty"`
	Enabled bool   `json:"enabled"`
}

// Template is a bot without memory or transcript.
type Template struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Persona     string            `json:"persona,omitempty"`
	Style       Style             `json:"style,omitempty"`
	Proactivity string            `json:"proactivity,omitempty"`
	QuietStart  string            `json:"quiet_start,omitempty"`
	QuietEnd    string            `json:"quiet_end,omitempty"`
	Timezone    string            `json:"timezone,omitempty"`
	AvatarMIME  string            `json:"avatar_mime,omitempty"`
	Avatar      string            `json:"avatar,omitempty"`
	Routines    []TemplateRoutine `json:"routines,omitempty"`
}

// ExportTemplate reads profile, persona, and routines. Memory and the transcript stay out.
func ExportTemplate(b *Bot) (Template, error) {
	persona, err := b.ReadPersona()
	if err != nil {
		return Template{}, err
	}
	tpl := Template{
		Name:        b.Name(),
		Description: b.cfg.Description,
		Persona:     persona,
		Style:       b.StyleCopy(),
		Proactivity: b.cfg.Proactivity,
		QuietStart:  b.cfg.QuietStart,
		QuietEnd:    b.cfg.QuietEnd,
		Timezone:    b.cfg.Timezone,
	}
	if b.cfg.Avatar == AvatarCustom {
		data, mime, ok, err := b.AvatarBytes()
		if err != nil {
			return Template{}, err
		}
		if ok {
			tpl.AvatarMIME = mime
			tpl.Avatar = base64.StdEncoding.EncodeToString(data)
		}
	}
	list, err := (&link.Timer{Dir: b.RoutineDir()}).List()
	if err != nil {
		return Template{}, err
	}
	for _, r := range list {
		tpl.Routines = append(tpl.Routines, TemplateRoutine{
			ID: r.ID, Name: r.Name, Prompt: r.Prompt, Cron: r.Cron, At: r.At, Enabled: r.Enabled,
		})
	}
	return tpl, nil
}

// ImportTemplate creates a new bot from a template. links and owner come from the caller.
// The new transcript is empty and no memory is copied.
func ImportTemplate(root string, tpl Template, links []string, owner string) (*Bot, error) {
	name := flat(tpl.Name)
	if !ValidName(name) {
		return nil, fmt.Errorf("bot: invalid name %q", tpl.Name)
	}
	persona := strings.TrimSpace(tpl.Persona)
	if persona == "" {
		persona = DefaultPersona
	}
	b, err := Create(root, name, persona, links)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Bot, error) {
		_ = os.RemoveAll(Dir(root, name))
		return nil, err
	}
	if owner != "" {
		if err := b.SetOwners([]string{owner}); err != nil {
			return fail(err)
		}
	}
	desc, tone, length, emoji, lang := tpl.Description, tpl.Style.Tone, tpl.Style.Length, tpl.Style.Emoji, tpl.Style.Language
	pro, qs, qe, tz := tpl.Proactivity, tpl.QuietStart, tpl.QuietEnd, tpl.Timezone
	if err := b.ApplyProfile(ProfileUpdate{
		Description: &desc, Tone: &tone, Length: &length, Emoji: &emoji, Language: &lang,
		Proactivity: &pro, QuietStart: &qs, QuietEnd: &qe, Timezone: &tz,
	}); err != nil {
		return fail(err)
	}
	if tpl.Avatar != "" {
		raw, err := base64.StdEncoding.DecodeString(tpl.Avatar)
		if err != nil {
			return fail(err)
		}
		if err := b.SetAvatar(raw, tpl.AvatarMIME); err != nil {
			return fail(err)
		}
	}
	tm := &link.Timer{Dir: b.RoutineDir()}
	for _, r := range tpl.Routines {
		if err := tm.Save(link.Routine{
			ID: r.ID, Name: r.Name, Prompt: r.Prompt, Cron: r.Cron, At: r.At, Enabled: r.Enabled,
		}); err != nil {
			return fail(err)
		}
	}
	return b, nil
}

// WriteTemplate writes a template as indented JSON.
func WriteTemplate(path string, tpl Template) error {
	raw, err := fsutil.Marshal(tpl)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}

// ReadTemplate loads a template file.
func ReadTemplate(path string) (Template, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Template{}, err
	}
	var tpl Template
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tpl); err != nil {
		return Template{}, err
	}
	return tpl, nil
}

// Rename moves a bot directory to a new name.
// primary.json is rewritten when it pointed at the old name. Pin state is left as it is.
// The work directory moves with the bot when it exists.
func Rename(root, from, to string) error {
	from = flat(from)
	to = flat(to)
	if !ValidName(from) || !ValidName(to) {
		return fmt.Errorf("bot: invalid name %q", to)
	}
	if from == to {
		return nil
	}
	return fsutil.WithLock(primaryLock(root), func() error {
		if _, err := Load(root, from); err != nil {
			return err
		}
		if _, err := Load(root, to); err == nil {
			return fmt.Errorf("bot %q: %w", to, ErrExists)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		fromDir, toDir := Dir(root, from), Dir(root, to)
		closeStoreDir(fromDir)
		if err := os.Rename(fromDir, toDir); err != nil {
			return err
		}
		b, err := Load(root, to)
		if err != nil {
			_ = os.Rename(toDir, fromDir)
			return err
		}
		b.cfg.Name = to
		if err := b.SaveConfig(); err != nil {
			_ = os.Rename(toDir, fromDir)
			return err
		}
		if err := rewritePrimaryName(root, from, to); err != nil {
			b.cfg.Name = from
			_ = b.SaveConfig()
			_ = os.Rename(toDir, fromDir)
			return err
		}
		if err := moveWork(root, from, to); err != nil {
			_ = rewritePrimaryName(root, to, from)
			b.cfg.Name = from
			_ = b.SaveConfig()
			_ = os.Rename(toDir, fromDir)
			return err
		}
		return nil
	})
}

func rewritePrimaryName(root, from, to string) error {
	raw, err := os.ReadFile(primaryPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f primaryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return err
	}
	if f.Name != from {
		return nil
	}
	out, err := fsutil.Marshal(primaryFile{Name: to})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(primaryPath(root), out, 0600)
}

func moveWork(root, from, to string) error {
	src := work.BotDir(root, from)
	dst := work.BotDir(root, to)
	st, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("bot: work directory %q already exists", to)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func validateQuiet(start, end string) error {
	if start == "" && end == "" {
		return nil
	}
	if start == "" || end == "" {
		return errors.New("bot: quiet hours need both a start and an end")
	}
	if _, ok := parseHHMM(start); !ok {
		return fmt.Errorf("bot: bad quiet start %q", start)
	}
	if _, ok := parseHHMM(end); !ok {
		return fmt.Errorf("bot: bad quiet end %q", end)
	}
	if start == end {
		return errors.New("bot: quiet hours start and end are the same")
	}
	return nil
}

func parseHHMM(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func flat(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

func limitText(s string, max int) (string, error) {
	s = flat(s)
	if utf8.RuneCountInString(s) > max {
		return "", fmt.Errorf("longer than %d characters", max)
	}
	return s, nil
}

func oneOf(v string, allowed ...string) (string, error) {
	for _, a := range allowed {
		if v == a {
			return v, nil
		}
	}
	return "", fmt.Errorf("must be one of %s", strings.Join(allowed, ", "))
}

func hslHex(h, s, l float64) string {
	r, g, b := hslToRGB(h, s, l)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(h/60, 6)
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var rf, gf, bf float64
	switch {
	case hp < 1:
		rf, gf, bf = c, x, 0
	case hp < 2:
		rf, gf, bf = x, c, 0
	case hp < 3:
		rf, gf, bf = 0, c, x
	case hp < 4:
		rf, gf, bf = 0, x, c
	case hp < 5:
		rf, gf, bf = x, 0, c
	default:
		rf, gf, bf = c, 0, x
	}
	m := l - c/2
	return toByte(rf + m), toByte(gf + m), toByte(bf + m)
}

func toByte(v float64) uint8 {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return uint8(math.Round(v * 255))
}
