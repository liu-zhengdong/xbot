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
	"xbot/internal/work"
)

const (
	// AvatarCustom means bots/<name>/avatar holds an uploaded image.
	AvatarCustom = "custom"
	// AvatarGenerated means the browser draws a line-art portrait from AvatarSeed.
	AvatarGenerated = "generated"
	// AvatarGlyph is a reserved seed: the UI draws the geometric glyph instead.
	AvatarGlyph = "glyph"
	maxAvatar   = 1 << 20
	maxSeed     = 80
	// seedBad are characters a seed may not contain. They would end up in the
	// web UI, so markup and quotes are kept out.
	seedBad = "<>\"'&`"
)

// GenerateAvatar is the seam for a future portrait generator.
// Nil means an unset avatar is a low-saturation blob. The initial is optional.
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
	Timezone    *string
	// Role picks the working defaults (chat or dev). BackgroundMaxTime
	// overrides it: zero uses the role default, a negative value turns the
	// limit off.
	Role              *string
	BackgroundMaxTime *int
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
	if u.Timezone != nil {
		v := flat(*u.Timezone)
		if v != "" {
			if _, err := time.LoadLocation(v); err != nil {
				return fmt.Errorf("bot: timezone: %w", err)
			}
		}
		cfg.Timezone = v
	}
	if u.Role != nil {
		v, err := oneOf(flat(*u.Role), "", RoleChat, RoleDev)
		if err != nil {
			return fmt.Errorf("bot: role: %w", err)
		}
		cfg.Role = v
	}
	if u.BackgroundMaxTime != nil {
		// Negative turns the budget off, so the low end is negative here rather
		// than the usual 0. Any nonzero negative value means "no limit"; the
		// magnitude is meaningless.
		v := *u.BackgroundMaxTime
		if v < 0 {
			v = -1
		} else if err := checkRange("background_max_time", v, 0, 24*3600); err != nil {
			return err
		}
		cfg.BackgroundMaxTime = v
	}
	b.cfg = cfg
	return b.SaveConfig()
}

// checkRange validates one working-limit override. A value of 0 means "use the
// role default", so the range starts there; a passed 0 is stored as 0.
func checkRange(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return fmt.Errorf("bot: %s: must be between %d and %d", name, lo, hi)
	}
	return nil
}

// ProfileText describes display metadata for the environment fingerprint only.
// An empty profile returns an empty fingerprint input.
func (b *Bot) ProfileText() string {
	if d := flat(b.cfg.Description); d != "" {
		return "About: " + d
	}
	return ""
}

// ComposedPersona returns the effective duties/persona. Display metadata such
// as the description never becomes instructions or replaces the owner's text.
func (b *Bot) ComposedPersona() (string, error) {
	return b.PersonaText()
}

// AvatarColors is the fallback fill: a low-saturation color from the name, plus an optional initial.
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

// ValidAvatarSeed reports whether seed can drive a portrait. The server only
// stores it; the browser draws from it.
func ValidAvatarSeed(seed string) bool {
	seed = strings.TrimSpace(seed)
	if !utf8.ValidString(seed) {
		return false
	}
	n := utf8.RuneCountInString(seed)
	if n < 1 || n > maxSeed {
		return false
	}
	for _, r := range seed {
		if unicode.IsControl(r) || strings.ContainsRune(seedBad, r) {
			return false
		}
	}
	return true
}

// AvatarState is what the UI should show. An old or unset avatar is drawn
// from the name, so it needs no migration. av is never empty.
func (b *Bot) AvatarState() (avatar, seed string) {
	avatar, seed = b.cfg.Avatar, b.cfg.AvatarSeed
	if avatar == "" {
		avatar = AvatarGenerated
	}
	if seed == "" {
		seed = b.cfg.Name
	}
	return avatar, seed
}

// SetAvatarSeed stores a generated portrait seed and drops any upload.
func (b *Bot) SetAvatarSeed(seed string) error {
	if !ValidAvatarSeed(seed) {
		return fmt.Errorf("bot: avatar seed must be 1–%d characters without <>\"'&` or control characters", maxSeed)
	}
	_ = os.Remove(b.AvatarPath())
	_ = os.Remove(b.avatarMIMEPath())
	b.cfg.Avatar = AvatarGenerated
	b.cfg.AvatarSeed = strings.TrimSpace(seed)
	return b.SaveConfig()
}

// SetAvatar stores an uploaded image and marks the avatar custom.
// AvatarSeed stays as it was, so clearing goes back to the previous seed.
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

// ClearAvatar removes an upload. The portrait drawn from the seed is used again.
func (b *Bot) ClearAvatar() error {
	_, seed := b.AvatarState()
	_ = os.Remove(b.AvatarPath())
	_ = os.Remove(b.avatarMIMEPath())
	b.cfg.Avatar = AvatarGenerated
	b.cfg.AvatarSeed = seed
	return b.SaveConfig()
}

// AvatarBytes returns a custom image, or a generated portrait when the seam is set.
// ok is false when the caller should draw the portrait from the seed.
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
	Zone    string `json:"zone,omitempty"`
}

// Template is a bot without memory or transcript.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Persona     string `json:"persona,omitempty"`
	// Style, Proactivity and the quiet hours are accepted on old exported templates and ignored.
	Style       json.RawMessage `json:"style,omitempty"`
	Proactivity string          `json:"proactivity,omitempty"`
	QuietStart  string          `json:"quiet_start,omitempty"`
	QuietEnd    string          `json:"quiet_end,omitempty"`
	Timezone    string          `json:"timezone,omitempty"`
	AvatarMIME  string          `json:"avatar_mime,omitempty"`
	// Avatar is the base64 upload; it is only set for a custom avatar.
	Avatar     string `json:"avatar,omitempty"`
	AvatarSeed string `json:"avatar_seed,omitempty"`
	// Routines run after the avatar is set.
	Routines []TemplateRoutine `json:"routines,omitempty"`
}

// ExportTemplate reads profile, persona, and routines. Memory and the transcript stay out.
// The persona is the text in effect: a picked card's body is copied in, so the
// template does not depend on this owner's card ids.
func ExportTemplate(b *Bot) (Template, error) {
	persona, err := b.PersonaText()
	if err != nil {
		return Template{}, err
	}
	tpl := Template{
		Name:        b.Name(),
		Description: b.cfg.Description,
		Persona:     persona,
		Timezone:    b.cfg.Timezone,
	}
	_, tpl.AvatarSeed = b.AvatarState()
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
			ID: r.ID, Name: r.Name, Prompt: r.Prompt, Cron: r.Cron, At: r.At, Enabled: r.Enabled, Zone: r.Zone,
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
	desc, tz := tpl.Description, tpl.Timezone
	if err := b.ApplyProfile(ProfileUpdate{Description: &desc, Timezone: &tz}); err != nil {
		return fail(err)
	}
	// The seed follows the template even when the name changed. A bad seed is
	// ignored, which leaves the new name as the seed.
	if ValidAvatarSeed(tpl.AvatarSeed) {
		if err := b.SetAvatarSeed(tpl.AvatarSeed); err != nil {
			return fail(err)
		}
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
			ID: r.ID, Name: r.Name, Prompt: r.Prompt, Cron: r.Cron, At: r.At, Enabled: r.Enabled, Zone: r.Zone,
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
	if !ValidName(from) || !ValidName(to) || !fsutil.PortableName(to) {
		return fmt.Errorf("bot: invalid name %q", to)
	}
	if from == to {
		return nil
	}
	return fsutil.WithLock(primaryLock(root), func() error {
		if _, err := Load(root, from); err != nil {
			return err
		}
		fromDir, toDir := Dir(root, from), Dir(root, to)
		// On a case-insensitive file system (macOS, Windows) a rename that only
		// changes case finds the bot itself under the new name. That is not a
		// collision; os.Rename changes the case in place.
		if _, err := Load(root, to); err == nil && !samePath(fromDir, toDir) {
			return fmt.Errorf("bot %q: %w", to, ErrExists)
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		closeStoreDir(fromDir)
		// Move the vault key before the directory so a failure leaves the bot
		// and its key under the same name. A missing key is fine.
		if err := moveVaultKey(root, from, to); err != nil {
			return err
		}
		if err := os.Rename(fromDir, toDir); err != nil {
			_ = moveVaultKey(root, to, from)
			return err
		}
		b, err := Load(root, to)
		if err != nil {
			_ = os.Rename(toDir, fromDir)
			_ = moveVaultKey(root, to, from)
			return err
		}
		// An old bot's portrait was drawn from its name; pin that seed so the
		// face survives the rename.
		if b.cfg.AvatarSeed == "" {
			b.cfg.AvatarSeed = from
		}
		b.cfg.Name = to
		if err := b.SaveConfig(); err != nil {
			_ = os.Rename(toDir, fromDir)
			_ = moveVaultKey(root, to, from)
			return err
		}
		if err := rewritePrimaryName(root, from, to); err != nil {
			b.cfg.Name = from
			_ = b.SaveConfig()
			_ = os.Rename(toDir, fromDir)
			_ = moveVaultKey(root, to, from)
			return err
		}
		if err := moveWork(root, from, to); err != nil {
			_ = rewritePrimaryName(root, to, from)
			b.cfg.Name = from
			_ = b.SaveConfig()
			_ = os.Rename(toDir, fromDir)
			_ = moveVaultKey(root, to, from)
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
