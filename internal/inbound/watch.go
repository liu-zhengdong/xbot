package inbound

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

// Watch is one glob. Events carry path metadata, never file contents.
type Watch struct {
	ID      string `json:"id"`
	Glob    string `json:"glob"`
	Enabled bool   `json:"enabled"`
}

// AddWatch stores a glob and grants it. The path must be absolute and must not contain "..".
func AddWatch(b *bot.Bot, id, pattern string) (Watch, error) {
	if b == nil {
		return Watch{}, errors.New("watch: nil bot")
	}
	if !ValidID(id) {
		return Watch{}, errors.New("watch: invalid id")
	}
	if err := CheckGlob(pattern); err != nil {
		return Watch{}, err
	}
	if _, err := LoadWatch(b, id); err == nil {
		return Watch{}, errors.New("watch: already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Watch{}, err
	}
	n := 0
	for _, name := range b.Config().Watches {
		if name != id {
			n++
		}
	}
	if n >= MaxWatches {
		return Watch{}, fmt.Errorf("watch: at most %d watches", MaxWatches)
	}
	w := Watch{ID: id, Glob: pattern, Enabled: true}
	if err := saveWatch(b, w); err != nil {
		return Watch{}, err
	}
	if err := b.GrantWatch(id); err != nil {
		return Watch{}, err
	}
	return w, nil
}

// LoadWatch reads one watch file.
func LoadWatch(b *bot.Bot, id string) (Watch, error) {
	if !ValidID(id) {
		return Watch{}, errors.New("watch: invalid id")
	}
	raw, err := os.ReadFile(watchPath(b, id))
	if err != nil {
		return Watch{}, err
	}
	var w Watch
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return Watch{}, err
	}
	if err := CheckGlob(w.Glob); err != nil {
		return Watch{}, err
	}
	return w, nil
}

// ListWatches returns granted watches in config order.
func ListWatches(b *bot.Bot) ([]Watch, error) {
	var out []Watch
	for _, id := range b.Config().Watches {
		w, err := LoadWatch(b, id)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// RemoveWatch deletes the glob and the grant.
func RemoveWatch(b *bot.Bot, id string) error {
	if !ValidID(id) {
		return errors.New("watch: invalid id")
	}
	if err := os.Remove(watchPath(b, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return b.RevokeWatch(id)
}

func saveWatch(b *bot.Bot, w Watch) error {
	raw, err := fsutil.Marshal(w)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(watchPath(b, w.ID), raw, 0600)
}

func watchPath(b *bot.Bot, id string) string {
	return filepath.Join(b.WatchDir(), "id", id+".json")
}

func statePath(b *bot.Bot) string {
	return filepath.Join(b.WatchDir(), "state.json")
}

// CheckGlob rejects relative paths, "..", and a pattern whose walk would start at the filesystem root.
func CheckGlob(pattern string) error {
	if pattern == "" || !filepath.IsAbs(pattern) {
		return errors.New("watch: path must be absolute")
	}
	if len(pattern) > 4096 {
		return errors.New("watch: path too long")
	}
	if hasDotDot(pattern) {
		return errors.New("watch: path must not contain ..")
	}
	root := globRoot(pattern)
	if root == "" || root == string(os.PathSeparator) {
		return errors.New("watch: refusing to watch the filesystem root")
	}
	return nil
}

func hasDotDot(p string) bool {
	for _, part := range strings.Split(p, string(os.PathSeparator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

func globRoot(pattern string) string {
	i := strings.IndexAny(pattern, "*?[")
	if i < 0 {
		return filepath.Clean(pattern)
	}
	prefix := pattern[:i]
	j := strings.LastIndex(prefix, string(os.PathSeparator))
	if j <= 0 {
		return string(os.PathSeparator)
	}
	return filepath.Clean(prefix[:j])
}

// DeliverFeeds posts due file-watch events for one bot.
// No configured watches is a no-op. A debounce of 0 uses DefaultDebounce.
func DeliverFeeds(b *bot.Bot, now time.Time, debounce time.Duration) error {
	if b == nil || len(b.Config().Watches) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	debounce = durationOr(debounce, DefaultDebounce)
	maxHold := debounce * 10
	return fsutil.WithLock(filepath.Join(b.WatchDir(), ".lock"), func() error {
		st, err := loadState(b)
		if err != nil {
			return err
		}
		next := st.clone()
		var events []link.Event
		dirty := false
		for _, id := range b.Config().Watches {
			w, err := LoadWatch(b, id)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !w.Enabled {
				continue
			}
			files, err := matchFiles(w.Glob)
			if err != nil {
				return err
			}
			due, changed := next.observe(id, files, now, debounce, maxHold)
			if changed {
				dirty = true
			}
			if len(due) == 0 {
				continue
			}
			events = append(events, watchEvent(id, due, now))
			next.commit(id, due)
			dirty = true
		}
		if len(events) > 0 {
			box, err := inbox.Open(b.InboxPath())
			if err != nil {
				return err
			}
			for _, ev := range events {
				if err := box.Post(ev); err != nil {
					return err
				}
			}
		}
		if !dirty {
			return nil
		}
		return saveState(b, next)
	})
}

// DeliverAll posts due watch events for every bot that has a watch configured.
func DeliverAll(root string, now time.Time, debounce time.Duration) error {
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		if len(cfg.Watches) == 0 {
			continue
		}
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			return err
		}
		if err := DeliverFeeds(b, now, debounce); err != nil {
			return err
		}
	}
	return nil
}

type fileMeta struct {
	Path  string
	Size  int64
	Mtime time.Time
}

func matchFiles(pattern string) ([]fileMeta, error) {
	if err := CheckGlob(pattern); err != nil {
		return nil, err
	}
	root := globRoot(pattern)
	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		if !matchPath(pattern, root) {
			return nil, nil
		}
		parent := filepath.Dir(root)
		if info.Mode()&os.ModeSymlink != 0 && symlinkEscapes(root, parent) {
			return nil, nil
		}
		return []fileMeta{metaOf(root, info)}, nil
	}
	var out []fileMeta
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root || d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 && symlinkEscapes(path, root) {
			return nil
		}
		if !matchPath(pattern, path) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, metaOf(path, fi))
		if len(out) >= MaxWatchFiles {
			return errCap
		}
		return nil
	})
	if err != nil && !errors.Is(err, errCap) {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func metaOf(path string, fi os.FileInfo) fileMeta {
	return fileMeta{Path: path, Size: fi.Size(), Mtime: fi.ModTime().UTC()}
}

var errCap = errors.New("watch: file cap")

func matchPath(pattern, path string) bool {
	return matchSegs(splitPath(pattern), splitPath(path))
}

func splitPath(p string) []string {
	p = filepath.Clean(p)
	parts := strings.Split(p, string(os.PathSeparator))
	var out []string
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func matchSegs(pat, segs []string) bool {
	for {
		if len(pat) == 0 {
			return len(segs) == 0
		}
		if pat[0] == "**" {
			if matchSegs(pat[1:], segs) {
				return true
			}
			if len(segs) == 0 {
				return false
			}
			segs = segs[1:]
			continue
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := filepath.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		segs = segs[1:]
	}
}

func symlinkEscapes(path, root string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved = filepath.Clean(root)
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

type snap struct {
	Size    int64 `json:"size"`
	MtimeNS int64 `json:"mtime_ns"`
}

type pend struct {
	Kind    string    `json:"kind"`
	Size    int64     `json:"size"`
	MtimeNS int64     `json:"mtime_ns"`
	Since   time.Time `json:"since"`
	Last    time.Time `json:"last"`
}

type watchState struct {
	Files   map[string]snap `json:"files,omitempty"`
	Pending map[string]pend `json:"pending,omitempty"`
	Seen    map[string]bool `json:"seen,omitempty"`
}

func (st watchState) clone() watchState {
	out := watchState{
		Files:   map[string]snap{},
		Pending: map[string]pend{},
		Seen:    map[string]bool{},
	}
	for k, v := range st.Files {
		out.Files[k] = v
	}
	for k, v := range st.Pending {
		out.Pending[k] = v
	}
	for k, v := range st.Seen {
		out.Seen[k] = v
	}
	return out
}

func (st *watchState) observe(id string, files []fileMeta, now time.Time, debounce, maxHold time.Duration) ([]change, bool) {
	if st.Files == nil {
		st.Files = map[string]snap{}
	}
	if st.Pending == nil {
		st.Pending = map[string]pend{}
	}
	if st.Seen == nil {
		st.Seen = map[string]bool{}
	}
	dirty := false
	if !st.Seen[id] {
		for _, f := range files {
			st.Files[stateKey(id, f.Path)] = snap{Size: f.Size, MtimeNS: f.Mtime.UnixNano()}
		}
		st.Seen[id] = true
		return nil, true
	}
	cur := map[string]fileMeta{}
	for _, f := range files {
		cur[f.Path] = f
		k := stateKey(id, f.Path)
		prev, ok := st.Files[k]
		if ok && prev.Size == f.Size && prev.MtimeNS == f.Mtime.UnixNano() {
			continue
		}
		kind := "modified"
		if !ok {
			kind = "created"
		}
		if st.touch(k, kind, f.Size, f.Mtime.UnixNano(), now) {
			dirty = true
		}
	}
	prefix := id + "\x00"
	for k := range st.Files {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		path := k[len(prefix):]
		if _, ok := cur[path]; ok {
			continue
		}
		if st.touch(k, "deleted", 0, 0, now) {
			dirty = true
		}
	}
	for k, p := range st.Pending {
		if !strings.HasPrefix(k, prefix) || p.Kind == "deleted" {
			continue
		}
		path := k[len(prefix):]
		if _, ok := cur[path]; ok {
			continue
		}
		if st.touch(k, "deleted", 0, 0, now) {
			dirty = true
		}
	}
	var due []change
	for k, p := range st.Pending {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if now.Before(p.Last.Add(debounce)) && now.Before(p.Since.Add(maxHold)) {
			continue
		}
		due = append(due, change{
			Path:  k[len(prefix):],
			Kind:  p.Kind,
			Size:  p.Size,
			Mtime: time.Unix(0, p.MtimeNS).UTC(),
			Since: p.Since,
		})
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Path < due[j].Path })
	return due, dirty
}

func (st *watchState) touch(key, kind string, size, mtime int64, now time.Time) bool {
	p, ok := st.Pending[key]
	if ok && p.Kind == kind && p.Size == size && p.MtimeNS == mtime {
		return false
	}
	if !ok || p.Since.IsZero() {
		p.Since = now
	}
	p.Kind = kind
	p.Size = size
	p.MtimeNS = mtime
	p.Last = now
	st.Pending[key] = p
	return true
}

func (st *watchState) commit(id string, due []change) {
	for _, c := range due {
		k := stateKey(id, c.Path)
		delete(st.Pending, k)
		if c.Kind == "deleted" {
			delete(st.Files, k)
			continue
		}
		st.Files[k] = snap{Size: c.Size, MtimeNS: c.Mtime.UnixNano()}
	}
}

func stateKey(id, path string) string { return id + "\x00" + path }

type change struct {
	Path  string
	Kind  string
	Size  int64
	Mtime time.Time
	Since time.Time
}

func watchEvent(id string, due []change, now time.Time) link.Event {
	kind := due[0].Kind
	for _, c := range due[1:] {
		if c.Kind != kind {
			kind = "changed"
			break
		}
	}
	ev := link.NewEvent("watch:"+id, kind, renderWatch(id, kind, due), "watch:"+id, now)
	ev.ID = watchEventID(id, due)
	return ev
}

func watchEventID(id string, due []change) string {
	h := sha256.New()
	for _, c := range due {
		fmt.Fprintf(h, "%s\n%s\n%d\n%d\n", c.Path, c.Kind, c.Size, c.Mtime.UnixNano())
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return "fw_" + id + "_" + sum[:16]
}

func renderWatch(id, kind string, due []change) string {
	shown := due
	extra := 0
	if len(shown) > maxWatchPaths {
		extra = len(shown) - maxWatchPaths
		shown = shown[:maxWatchPaths]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<watch id=%q kind=%q count=\"%d\">\n", id, kind, len(due))
	for _, c := range shown {
		mt := c.Mtime.UTC().Format(time.RFC3339)
		fmt.Fprintf(&b, "path: %s\nkind: %s\nsize: %d\nmtime: %s\n", c.Path, c.Kind, c.Size, mt)
	}
	if extra > 0 {
		fmt.Fprintf(&b, "and %d more\n", extra)
	}
	b.WriteString("</watch>\n")
	return b.String()
}

func loadState(b *bot.Bot) (watchState, error) {
	raw, err := os.ReadFile(statePath(b))
	if errors.Is(err, os.ErrNotExist) {
		return watchState{}, nil
	}
	if err != nil {
		return watchState{}, err
	}
	var st watchState
	if err := json.Unmarshal(raw, &st); err != nil {
		return watchState{}, err
	}
	return st, nil
}

func saveState(b *bot.Bot, st watchState) error {
	raw, err := fsutil.Marshal(st)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(statePath(b), raw, 0600)
}
