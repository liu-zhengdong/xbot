// Package personacard keeps the persona cards in ~/.xbot/persona-cards/<id>/card.md.
// A bot that picks a card reads its body as the persona on every turn, so editing a card
// changes every bot that uses it. The id names the directory only; people see the card name.
package personacard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"xbot/internal/fsutil"
)

const (
	// MaxBody matches the cap on persona.md.
	MaxBody = 32 << 10
	// MaxName is the card name in runes.
	MaxName = 60
	// MaxCards caps the library.
	MaxCards = 200
)

var (
	ErrNotFound = errors.New("personacard: not found")
	// ErrExists is returned by Import when a card already has that name and the caller did not confirm.
	ErrExists = errors.New("personacard: a card has this name")
	ErrFull   = errors.New("personacard: library is full")
	ErrEmpty  = errors.New("personacard: empty body")
	ErrLarge  = errors.New("personacard: body too long")
	// ErrDefault is returned when deleting the default card.
	ErrDefault = errors.New("personacard: the default card stays")

	idRe   = regexp.MustCompile(`^[a-z0-9]{8,32}$`)
	suffix = regexp.MustCompile(` \((\d+)\)$`)
)

// Card is one persona card.
type Card struct {
	ID      string
	Name    string
	Body    string
	Updated time.Time
}

// Dir is the library: one directory per card.
func Dir(root string) string { return filepath.Join(root, "persona-cards") }

// ValidID reports whether id can name a card directory.
func ValidID(id string) bool { return idRe.MatchString(id) }

// mu makes a check and the write after it one step inside this process;
// the library lock does the same across processes.
var mu sync.Mutex

// flat holds libraries already checked for per-owner subdirectories; guarded by mu.
var flat = map[string]bool{}

func locked(root string, fn func(dir string) error) error {
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		if err := liftOwners(dir); err != nil {
			return err
		}
		return fn(dir)
	})
}

// open returns the library for a read, first lifting any per-owner subdirectories
// under the library lock. A library already flat is not written to.
func open(root string) (string, error) {
	dir := Dir(root)
	mu.Lock()
	done := flat[dir] || len(owners(dir)) == 0
	flat[dir] = done
	mu.Unlock()
	if done {
		return dir, nil
	}
	return dir, locked(root, func(string) error { return nil })
}

// owners lists the subdirectories left by the old persona-cards/<owner>/<id>/ layout:
// a card directory holds card.md, an owner directory does not.
func owners(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !has(filepath.Join(dir, e.Name()), "card.md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// liftOwners moves every card of the old per-owner layout into the flat library and
// keeps its id, so bots bound to it still find it. An id already taken by a different
// card gets a new id instead of overwriting, and that owner's bots bound to the old id
// follow it to the new one; an identical copy is dropped. The first
// owner's default card becomes the library's default unless it already has one.
// Callers hold the library lock.
func liftOwners(dir string) error {
	if flat[dir] {
		return nil
	}
	for _, od := range owners(dir) {
		ents, err := os.ReadDir(od)
		if err != nil {
			return err
		}
		moved := map[string]string{}
		for _, e := range ents {
			src := filepath.Join(od, e.Name())
			if !e.IsDir() || !ValidID(e.Name()) || !has(src, "card.md") {
				continue
			}
			id := e.Name()
			if has(filepath.Join(dir, id), "card.md") {
				a, _ := os.ReadFile(filepath.Join(src, "card.md"))
				b, _ := os.ReadFile(filepath.Join(dir, id, "card.md"))
				if string(a) == string(b) {
					if err := os.RemoveAll(src); err != nil {
						return err
					}
					moved[e.Name()] = id
					continue
				}
				id = newID()
			}
			if err := os.Rename(src, filepath.Join(dir, id)); err != nil {
				return err
			}
			moved[e.Name()] = id
		}
		if err := rebind(filepath.Dir(dir), filepath.Base(od), moved); err != nil {
			return err
		}
		// Carry the markers after the cards, so a crash in between keeps them to lift again.
		if raw, err := os.ReadFile(filepath.Join(od, defaultFile)); err == nil && !has(dir, defaultFile) {
			if id, ok := moved[strings.TrimSpace(string(raw))]; ok {
				if err := fsutil.WriteAtomic(filepath.Join(dir, defaultFile), []byte(id+"\n"), 0600); err != nil {
					return err
				}
			}
		}
		if has(od, seededFile) {
			if err := markSeeded(dir); err != nil {
				return err
			}
		}
		for _, f := range []string{defaultFile, seededFile, ".lock"} {
			_ = os.Remove(filepath.Join(od, f))
		}
		_ = os.Remove(od)
	}
	flat[dir] = true
	return nil
}

// rebind points the bots of owner bound to a renamed card at its new id. Only bots
// with that single owner read the owner's library before, so only they move; the
// rest of each config is kept as it is on disk.
func rebind(root, owner string, moved map[string]string) error {
	ents, _ := os.ReadDir(filepath.Join(root, "bots"))
	for _, e := range ents {
		bd := filepath.Join(root, "bots", e.Name())
		if !e.IsDir() || !has(bd, "config.json") {
			continue
		}
		path := filepath.Join(bd, "config.json")
		err := fsutil.WithLock(path+".lock", func() error {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var cfg struct {
				Owners      []string `json:"owners"`
				PersonaCard string   `json:"persona_card"`
			}
			if json.Unmarshal(raw, &cfg) != nil || len(cfg.Owners) != 1 || cfg.Owners[0] != owner {
				return nil
			}
			id, ok := moved[cfg.PersonaCard]
			if !ok || id == cfg.PersonaCard {
				return nil
			}
			var all map[string]json.RawMessage
			if err := json.Unmarshal(raw, &all); err != nil {
				return err
			}
			all["persona_card"], _ = json.Marshal(id)
			out, err := fsutil.Marshal(all)
			if err != nil {
				return err
			}
			return fsutil.WriteAtomic(path, out, 0600)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// List returns the cards, most recently updated first. A missing library is empty.
func List(root string) ([]Card, error) {
	dir, err := open(root)
	if err != nil {
		return nil, err
	}
	return list(dir)
}

func list(dir string) ([]Card, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, e := range ents {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		c, err := read(dir, e.Name())
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Get reads one card.
func Get(root, id string) (Card, error) {
	if !ValidID(id) {
		return Card{}, ErrNotFound
	}
	dir, err := open(root)
	if err != nil {
		return Card{}, err
	}
	return read(dir, id)
}

// Create stores a new card and returns it.
func Create(root, name, body string) (Card, error) {
	name, body, err := clean(name, body)
	if err != nil {
		return Card{}, err
	}
	var c Card
	err = locked(root, func(dir string) error {
		cards, err := list(dir)
		if err != nil {
			return err
		}
		if len(cards) >= MaxCards {
			return ErrFull
		}
		if c, err = write(dir, newID(), name, body); err != nil {
			return err
		}
		_ = markSeeded(dir)
		return nil
	})
	return c, err
}

// Update changes a card's name, body, or both. A nil field stays as it was.
func Update(root, id string, name, body *string) (Card, error) {
	if !ValidID(id) {
		return Card{}, ErrNotFound
	}
	var c Card
	err := locked(root, func(dir string) error {
		cur, err := read(dir, id)
		if err != nil {
			return err
		}
		n, b := cur.Name, cur.Body
		if name != nil {
			n = *name
		}
		if body != nil {
			b = *body
		}
		if n, b, err = clean(n, b); err != nil {
			return err
		}
		if c, err = write(dir, id, n, b); err != nil {
			return err
		}
		_ = markSeeded(dir)
		return nil
	})
	return c, err
}

// Delete removes a card. Unbinding the bots that use it is the caller's job.
func Delete(root, id string) error {
	return DeleteWith(root, id, nil)
}

// DeleteWith runs before under the library lock with the card known to exist,
// then marks the library as used and removes the card unless before failed.
// Callers unbind bots in before, so a concurrent pick (see With) cannot bind
// the card after the check.
func DeleteWith(root, id string, before func() error) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	return locked(root, func(dir string) error {
		if _, err := os.Stat(filepath.Join(dir, id, "card.md")); err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		if isDefault(dir, id) {
			return ErrDefault
		}
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		// The card existed, so the library has had cards: record it before the last
		// one can go, or a library from before the marker would get the seeds again.
		if err := markSeeded(dir); err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(dir, id))
	})
}

// With runs fn under the library lock with the card known to exist, so the
// card cannot be deleted between the check and what fn does with it.
func With(root, id string, fn func(Card) error) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	return locked(root, func(dir string) error {
		c, err := read(dir, id)
		if err != nil {
			return err
		}
		return fn(c)
	})
}

// Import always makes a new card from a Markdown file. The name is the first non-empty line,
// or the file name without its extension when that line is too long to be a name.
// Without confirm, a name already in the library returns ErrExists with that name;
// with confirm, the new card gets the next free " (2)" style suffix. Nothing is overwritten.
func Import(root, filename, content string, confirm bool) (Card, error) {
	name := importName(filename, content)
	_, body, err := clean(name, content)
	if err != nil {
		return Card{Name: name}, err
	}
	var c Card
	err = locked(root, func(dir string) error {
		cards, err := list(dir)
		if err != nil {
			return err
		}
		if len(cards) >= MaxCards {
			return ErrFull
		}
		taken := map[string]bool{}
		for _, x := range cards {
			taken[x.Name] = true
		}
		if taken[name] {
			if !confirm {
				c = Card{Name: name}
				return ErrExists
			}
			name = nextFree(name, taken)
		}
		if c, err = write(dir, newID(), name, body); err != nil {
			return err
		}
		_ = markSeeded(dir)
		return nil
	})
	return c, err
}

func nextFree(name string, taken map[string]bool) string {
	base, n := name, 2
	if m := suffix.FindStringSubmatchIndex(name); m != nil {
		if v, err := strconv.Atoi(name[m[2]:m[3]]); err == nil {
			base, n = name[:m[0]], v+1
		}
	}
	for ; ; n++ {
		next := clipName(base, " ("+strconv.Itoa(n)+")")
		if !taken[next] {
			return next
		}
	}
}

func clipName(base, tail string) string {
	r := []rune(base)
	if max := MaxName - utf8.RuneCountInString(tail); len(r) > max {
		r = r[:max]
	}
	return strings.TrimSpace(string(r)) + tail
}

func importName(filename, content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line == "" || line == "---" {
			continue
		}
		if utf8.RuneCountInString(line) <= MaxName {
			return oneLine(line)
		}
		break
	}
	base := filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if n := oneLine(base); n != "" && n != "." {
		return clipName(n, "")
	}
	return "导入的人设卡"
}

func clean(name, body string) (string, string, error) {
	name = oneLine(name)
	if name == "" {
		name = "未命名人设卡"
	}
	if utf8.RuneCountInString(name) > MaxName {
		name = clipName(name, "")
	}
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return name, "", ErrEmpty
	}
	if len(body) > MaxBody {
		return name, "", ErrLarge
	}
	if !utf8.ValidString(body) {
		return name, "", errors.New("personacard: body is not text")
	}
	return name, body, nil
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// The file is a name line, a blank line, then the body. The name is one line by construction.
func write(dir, id, name, body string) (Card, error) {
	raw := "name: " + name + "\n\n" + body + "\n"
	path := filepath.Join(dir, id, "card.md")
	if err := fsutil.WriteAtomic(path, []byte(raw), 0600); err != nil {
		return Card{}, err
	}
	c := Card{ID: id, Name: name, Body: body, Updated: time.Now().UTC()}
	if st, err := os.Stat(path); err == nil {
		c.Updated = st.ModTime().UTC()
	}
	return c, nil
}

func read(dir, id string) (Card, error) {
	path := filepath.Join(dir, id, "card.md")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Card{}, err
	}
	if st.Size() > MaxBody+4096 {
		return Card{}, fmt.Errorf("personacard: %s is too large", id)
	}
	buf, err := io.ReadAll(io.LimitReader(f, MaxBody+4096))
	if err != nil {
		return Card{}, err
	}
	head, body, _ := strings.Cut(string(buf), "\n")
	name, ok := strings.CutPrefix(head, "name: ")
	if !ok {
		return Card{}, fmt.Errorf("personacard: %s has no name", id)
	}
	return Card{ID: id, Name: strings.TrimSpace(name), Body: strings.TrimSpace(body), Updated: st.ModTime().UTC()}, nil
}
