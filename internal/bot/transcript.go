package bot

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/clip"
	"xbot/internal/fsutil"
	"xbot/internal/provider"

	_ "modernc.org/sqlite"
)

const (
	// SegmentMaxBytes is the default size that seals a transcript segment.
	// A new UTC day seals the tail even when it is smaller.
	defaultSegmentMax = 8 << 20
	// BlobThreshold is the default size past which a tool result is stored
	// out of line. The transcript keeps a clipped preview.
	defaultBlobThreshold = 4096
	blobPreview          = 512
)

type segStore struct {
	dir           string
	db            *sql.DB
	mu            sync.Mutex
	segmentMax    int64
	blobThreshold int64
}

// SetSegmentMax sets the roll size for this bot. Zero restores the default.
func (b *Bot) SetSegmentMax(n int) {
	s := b.store()
	s.mu.Lock()
	s.segmentMax = int64(n)
	s.mu.Unlock()
}

// SetBlobThreshold sets the tool-output spill size for this bot. Zero restores the default.
func (b *Bot) SetBlobThreshold(n int) {
	s := b.store()
	s.mu.Lock()
	s.blobThreshold = int64(n)
	s.mu.Unlock()
}

func (s *segStore) segLimit() int64 {
	if s.segmentMax > 0 {
		return s.segmentMax
	}
	return defaultSegmentMax
}

func (s *segStore) blobLimit() int64 {
	if s.blobThreshold > 0 {
		return s.blobThreshold
	}
	return defaultBlobThreshold
}

var (
	storesMu sync.Mutex
	stores   = map[string]*segStore{}
)

func (b *Bot) store() *segStore {
	dir := b.dir()
	storesMu.Lock()
	defer storesMu.Unlock()
	if s, ok := stores[dir]; ok {
		return s
	}
	s := &segStore{dir: dir}
	stores[dir] = s
	return s
}

// CloseStore drops the cached index connection. The next read opens it again.
func CloseStore(b *Bot) {
	if b == nil {
		return
	}
	closeStoreDir(b.dir())
}

func closeStoreDir(dir string) {
	storesMu.Lock()
	s := stores[dir]
	delete(stores, dir)
	storesMu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	_ = s.closeDB()
	s.mu.Unlock()
}

func (s *segStore) with(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fsutil.WithLock(filepath.Join(s.dir, "transcript.lock"), func() error {
		if err := s.ensure(); err != nil {
			return err
		}
		return fn()
	})
}

func (s *segStore) transcriptDir() string { return filepath.Join(s.dir, "transcript") }

func (s *segStore) readyPath() string { return filepath.Join(s.transcriptDir(), "READY") }

func (s *segStore) legacyPath() string { return filepath.Join(s.dir, "transcript.jsonl") }

func (s *segStore) ensure() error {
	if err := os.MkdirAll(s.transcriptDir(), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(s.readyPath()); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.migrate(); err != nil {
			return err
		}
	} else if _, err := os.Stat(s.legacyPath()); err == nil {
		if err := os.Rename(s.legacyPath(), s.legacyPath()+".migrated"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := s.openDB(); err != nil {
		return err
	}
	return s.repair()
}

func (s *segStore) migrate() error {
	legacy := s.legacyPath()
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return writeSynced(s.readyPath(), []byte("ok\n"), 0600)
	} else if err != nil {
		return err
	}
	// A partial segmented directory is not committed until READY exists.
	_ = s.closeDB()
	if err := os.RemoveAll(s.transcriptDir()); err != nil {
		return err
	}
	if err := os.MkdirAll(s.transcriptDir(), 0700); err != nil {
		return err
	}
	if err := s.openDB(); err != nil {
		return err
	}
	var seq int
	err := fsutil.ReadJSONL(legacy, func(line []byte) error {
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		seq++
		return s.appendLocked(e, true)
	})
	if err != nil {
		_ = s.closeDB()
		_ = os.RemoveAll(s.transcriptDir())
		return err
	}
	if err := s.syncDir(s.transcriptDir()); err != nil {
		return err
	}
	if err := writeSynced(s.readyPath(), []byte("ok\n"), 0600); err != nil {
		return err
	}
	if err := s.syncDir(s.transcriptDir()); err != nil {
		return err
	}
	if err := os.Rename(legacy, legacy+".migrated"); err != nil {
		return err
	}
	_ = seq
	return nil
}

func (s *segStore) openDB() error {
	if s.db != nil {
		if err := s.db.Ping(); err == nil {
			return nil
		}
		_ = s.db.Close()
		s.db = nil
	}
	path := filepath.Join(s.transcriptDir(), "index.sqlite")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if err := initSchema(db); err != nil {
		_ = db.Close()
		_ = os.Rename(path, path+".bad")
		_ = os.Remove(path + "-wal")
		_ = os.Remove(path + "-shm")
		db, err = sql.Open("sqlite", sqliteDSN(path))
		if err != nil {
			return err
		}
		db.SetMaxOpenConns(1)
		if err := initSchema(db); err != nil {
			_ = db.Close()
			return err
		}
	}
	s.db = db
	return nil
}

func sqliteDSN(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS lines (
  seq INTEGER PRIMARY KEY,
  start INTEGER NOT NULL,
  off INTEGER NOT NULL,
  len INTEGER NOT NULL,
  event_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS lines_event ON lines(event_id) WHERE event_id != '';
CREATE TABLE IF NOT EXISTS open_tools (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  args TEXT NOT NULL,
  call_seq INTEGER NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(body, tokenize='unicode61', content='', contentless_delete=1);
`)
	return err
}

func (s *segStore) closeDB() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *segStore) repair() error {
	var seq, start, end int
	err := s.db.QueryRow(`SELECT seq, start, off+len FROM lines ORDER BY seq DESC LIMIT 1`).Scan(&seq, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		segs, err := s.segments()
		if err != nil {
			return err
		}
		for _, seg := range segs {
			fi, err := os.Stat(seg.path)
			if err != nil {
				return err
			}
			if fi.Size() > 0 {
				return s.rebuild()
			}
		}
		return nil
	}
	if err != nil {
		return s.rebuild()
	}
	path, err := s.fileFor(start)
	if err != nil {
		return s.rebuild()
	}
	fi, err := os.Stat(path)
	if err != nil {
		return s.rebuild()
	}
	if fi.Size() < int64(end) {
		return s.rebuild()
	}
	if fi.Size() == int64(end) {
		return nil
	}
	return s.indexFrom(path, start, int64(end), seq)
}

func (s *segStore) indexFrom(path string, start int, off int64, seq int) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		// Sealed segments are read-only. Extra bytes only belong on the tail.
		if !strings.HasSuffix(filepath.Base(path), "-open.jsonl") {
			return s.rebuild()
		}
		return err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	body := rest
	if len(body) == 0 {
		return nil
	}
	if body[len(body)-1] != '\n' {
		i := bytes.LastIndexByte(body, '\n')
		if i < 0 {
			if err := f.Truncate(off); err != nil {
				return err
			}
			return f.Sync()
		}
		if err := f.Truncate(off + int64(i+1)); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		body = body[:i+1]
	}
	lines := bytes.Split(body, []byte("\n"))
	pos := off
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		raw := append(append([]byte{}, line...), '\n')
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		seq++
		if err := s.index(seq, start, pos, len(raw), e, s.indexBody(e)); err != nil {
			return err
		}
		pos += int64(len(raw))
	}
	return nil
}

func (s *segStore) rebuild() error {
	if _, err := s.db.Exec(`DELETE FROM lines; DELETE FROM open_tools;`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO fts(fts) VALUES('delete-all')`); err != nil {
		return err
	}
	segs, err := s.segments()
	if err != nil {
		return err
	}
	seq := 0
	for _, seg := range segs {
		f, err := os.Open(seg.path)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		if data[len(data)-1] != '\n' {
			i := bytes.LastIndexByte(data, '\n')
			if i < 0 {
				continue
			}
			data = data[:i+1]
		}
		pos := 0
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			rawLen := len(line) + 1
			var e Entry
			if err := json.Unmarshal(line, &e); err != nil {
				return err
			}
			seq++
			if err := s.index(seq, seg.start, int64(pos), rawLen, e, s.indexBody(e)); err != nil {
				return err
			}
			pos += rawLen
		}
	}
	return nil
}

// Reindex rebuilds the SQLite index from the segment files.
func (b *Bot) Reindex() error {
	return b.store().with(func() error { return b.store().rebuild() })
}

func (s *segStore) append(e Entry, indexIt bool) error {
	return s.with(func() error { return s.appendLocked(e, indexIt) })
}

func (s *segStore) appendLocked(e Entry, indexIt bool) error {
	full := ftsBody(e)
	e = spillBlob(s.transcriptDir(), e, s.blobLimit())
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	line, err := marshalEntry(e)
	if err != nil {
		return err
	}
	if err := s.roll(e.Time, len(line)); err != nil {
		return err
	}
	tail, err := s.openTail(e.Time)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tail.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	off, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if !indexIt {
		return nil
	}
	seq, err := s.maxSeq()
	if err != nil {
		return err
	}
	return s.index(seq+1, tail.start, off, len(line), e, full)
}

// appendSkipIndex writes a durable line and leaves the index behind.
// The next open repairs it. Tests use this to simulate a crash mid-index.
func (b *Bot) appendSkipIndex(e Entry) error {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	return b.store().append(e, false)
}

func (s *segStore) index(seq, start int, off int64, n int, e Entry, body string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO lines(seq, start, off, len, event_id, role) VALUES (?,?,?,?,?,?)`,
		seq, start, off, n, e.EventID, e.Message.Role); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO fts(rowid, body) VALUES (?,?)`, seq, body); err != nil {
		return err
	}
	if e.Message.Role == provider.RoleAssistant {
		for _, tc := range e.Message.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO open_tools(id, name, args, call_seq) VALUES (?,?,?,?)`,
				tc.ID, tc.Function.Name, tc.Function.Arguments, seq); err != nil {
				return err
			}
		}
	}
	if e.Message.Role == provider.RoleTool && e.Message.ToolCallID != "" {
		if _, err := tx.Exec(`DELETE FROM open_tools WHERE id=?`, e.Message.ToolCallID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type tail struct {
	start int
	day   string
	path  string
}

func (s *segStore) roll(at time.Time, lineLen int) error {
	t, err := s.currentTail()
	if err != nil || t == nil {
		return err
	}
	fi, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	day := at.UTC().Format("2006-01-02")
	if fi.Size() == 0 {
		if day != t.day {
			t.day = day
			return s.writeTailMeta(t)
		}
		return nil
	}
	limit := s.segLimit()
	if day != t.day || fi.Size()+int64(lineLen) > limit {
		return s.seal(t)
	}
	return nil
}

func (s *segStore) seal(t *tail) error {
	end, err := s.maxSeq()
	if err != nil {
		return err
	}
	if end < t.start {
		return nil
	}
	sealed := filepath.Join(s.transcriptDir(), fmt.Sprintf("seg-%012d-%012d.jsonl", t.start, end))
	if err := os.Rename(t.path, sealed); err != nil {
		return err
	}
	if err := os.Chmod(sealed, 0444); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.transcriptDir(), "tail.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.syncDir(s.transcriptDir())
}

func (s *segStore) openTail(at time.Time) (*tail, error) {
	t, err := s.currentTail()
	if err != nil || t != nil {
		return t, err
	}
	seq, err := s.maxSeq()
	if err != nil {
		return nil, err
	}
	start := seq + 1
	path := filepath.Join(s.transcriptDir(), fmt.Sprintf("seg-%012d-open.jsonl", start))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	t = &tail{start: start, day: at.UTC().Format("2006-01-02"), path: path}
	if err := s.writeTailMeta(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *segStore) writeTailMeta(t *tail) error {
	raw, err := json.Marshal(tailMeta{Start: t.start, Day: t.day})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(s.transcriptDir(), "tail.json"), append(raw, '\n'), 0600)
}

type tailMeta struct {
	Start int    `json:"start"`
	Day   string `json:"day"`
}

func (s *segStore) currentTail() (*tail, error) {
	raw, err := os.ReadFile(filepath.Join(s.transcriptDir(), "tail.json"))
	if errors.Is(err, os.ErrNotExist) {
		matches, err := filepath.Glob(filepath.Join(s.transcriptDir(), "seg-*-open.jsonl"))
		if err != nil || len(matches) == 0 {
			return nil, err
		}
		start, err := startOf(matches[0])
		if err != nil {
			return nil, err
		}
		return &tail{start: start, day: time.Now().UTC().Format("2006-01-02"), path: matches[0]}, nil
	}
	if err != nil {
		return nil, err
	}
	var meta tailMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	path := filepath.Join(s.transcriptDir(), fmt.Sprintf("seg-%012d-open.jsonl", meta.Start))
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return &tail{start: meta.Start, day: meta.Day, path: path}, nil
}

func (s *segStore) maxSeq() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM lines`).Scan(&n)
	return n, err
}

func (s *segStore) entries() ([]Entry, error) {
	var out []Entry
	err := s.with(func() error {
		n, err := s.maxSeq()
		if err != nil || n == 0 {
			return err
		}
		out, err = s.slice(1, n)
		return err
	})
	return out, err
}

// Count is the number of transcript records.
func (b *Bot) Count() (int, error) {
	var n int
	err := b.store().with(func() error {
		var err error
		n, err = b.store().maxSeq()
		return err
	})
	return n, err
}

// Slice returns inclusive sequence numbers. Stored previews are not hydrated.
func (b *Bot) Slice(from, to int) ([]Entry, error) {
	var out []Entry
	err := b.store().with(func() error {
		var err error
		out, err = b.store().slice(from, to)
		return err
	})
	return out, err
}

func (s *segStore) slice(from, to int) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT seq, start, off, len FROM lines WHERE seq BETWEEN ? AND ? ORDER BY seq`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type loc struct {
		seq, start, n int
		off           int64
	}
	var locs []loc
	for rows.Next() {
		var l loc
		if err := rows.Scan(&l.seq, &l.start, &l.off, &l.n); err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(locs))
	files := map[int]*os.File{}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	for _, l := range locs {
		f := files[l.start]
		if f == nil {
			path, err := s.fileFor(l.start)
			if err != nil {
				return nil, err
			}
			f, err = os.Open(path)
			if err != nil {
				return nil, err
			}
			files[l.start] = f
		}
		buf := make([]byte, l.n)
		if _, err := f.ReadAt(buf, l.off); err != nil {
			return nil, err
		}
		if len(buf) > 0 && buf[len(buf)-1] == '\n' {
			buf = buf[:len(buf)-1]
		}
		var e Entry
		if err := json.Unmarshal(buf, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Walk calls fn for each entry in the inclusive range, one segment at a time.
// to <= 0 means through the last record. fn sees the stored preview, not the blob.
func (b *Bot) Walk(from, to int, fn func(seq int, e Entry) error) error {
	return b.store().with(func() error {
		n, err := b.store().maxSeq()
		if err != nil {
			return err
		}
		if n == 0 || from > n {
			return nil
		}
		if from < 1 {
			from = 1
		}
		if to <= 0 || to > n {
			to = n
		}
		segs, err := b.store().segments()
		if err != nil {
			return err
		}
		for _, seg := range segs {
			if seg.end > 0 && seg.end < from {
				continue
			}
			if seg.start > to {
				break
			}
			rows, err := b.store().db.Query(`SELECT seq, off, len FROM lines WHERE start=? AND seq BETWEEN ? AND ? ORDER BY seq`, seg.start, from, to)
			if err != nil {
				return err
			}
			f, err := os.Open(seg.path)
			if err != nil {
				rows.Close()
				return err
			}
			for rows.Next() {
				var seq, n int
				var off int64
				if err := rows.Scan(&seq, &off, &n); err != nil {
					f.Close()
					rows.Close()
					return err
				}
				buf := make([]byte, n)
				if _, err := f.ReadAt(buf, off); err != nil {
					f.Close()
					rows.Close()
					return err
				}
				if len(buf) > 0 && buf[len(buf)-1] == '\n' {
					buf = buf[:len(buf)-1]
				}
				var e Entry
				if err := json.Unmarshal(buf, &e); err != nil {
					f.Close()
					rows.Close()
					return err
				}
				if err := fn(seq, e); err != nil {
					f.Close()
					rows.Close()
					return err
				}
			}
			err = rows.Err()
			f.Close()
			rows.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Search returns matching sequence numbers, best BM25 hit first.
func (b *Bot) Search(query string, limit int) ([]int, error) {
	q := ftsQuery(query)
	if q == "" || limit <= 0 {
		return nil, nil
	}
	var seqs []int
	err := b.store().with(func() error {
		rows, err := b.store().db.Query(`SELECT rowid FROM fts WHERE fts MATCH ? ORDER BY bm25(fts) LIMIT ?`, q, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var seq int
			if err := rows.Scan(&seq); err != nil {
				return err
			}
			seqs = append(seqs, seq)
		}
		return rows.Err()
	})
	return seqs, err
}

// FindEvent returns the 1-based sequence of a user line with this event id, or 0.
func (b *Bot) FindEvent(id string) (int, error) {
	if id == "" {
		return 0, nil
	}
	var seq int
	err := b.store().with(func() error {
		err := b.store().db.QueryRow(`SELECT seq FROM lines WHERE event_id=? AND role=? ORDER BY seq LIMIT 1`, id, provider.RoleUser).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			seq = 0
			return nil
		}
		return err
	})
	return seq, err
}

// OpenTool is a tool call at the tail that has no result yet.
type OpenTool struct {
	ID      string
	Name    string
	Args    string
	CallSeq int
}

// OpenTools lists tool calls that have not received a result.
func (b *Bot) OpenTools() ([]OpenTool, error) {
	var out []OpenTool
	err := b.store().with(func() error {
		rows, err := b.store().db.Query(`SELECT id, name, args, call_seq FROM open_tools ORDER BY call_seq`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t OpenTool
			if err := rows.Scan(&t.ID, &t.Name, &t.Args, &t.CallSeq); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Uncovered returns transcript lines that no active summary block covers.
func (b *Bot) Uncovered(blocks []SummaryBlock) ([]Entry, error) {
	n, err := b.Count()
	if err != nil || n == 0 {
		return nil, err
	}
	var out []Entry
	for _, sp := range uncoveredSpans(n, blocks) {
		part, err := b.Slice(sp[0], sp[1])
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

// Hydrate replaces a spilled tool preview with the full blob.
func (b *Bot) Hydrate(e Entry) (Entry, error) {
	return b.HydrateLimit(e, 0)
}

// HydrateLimit reads at most limit bytes of a spilled blob. Zero reads the whole blob.
// Inline content longer than a positive limit is cut to that many bytes.
func (b *Bot) HydrateLimit(e Entry, limit int) (Entry, error) {
	if e.Blob == "" {
		if limit > 0 && len(e.Message.Content) > limit {
			e.Message.Content = e.Message.Content[:limit]
		}
		return e, nil
	}
	f, err := os.Open(filepath.Join(b.dir(), "transcript", "blobs", e.Blob))
	if err != nil {
		return e, err
	}
	defer f.Close()
	var raw []byte
	if limit > 0 {
		raw = make([]byte, limit)
		n, err := io.ReadFull(f, raw)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return e, err
		}
		raw = raw[:n]
	} else {
		raw, err = io.ReadAll(f)
		if err != nil {
			return e, err
		}
	}
	e.Message.Content = string(raw)
	return e, nil
}

// ConcatRaw is the segment bytes in sequence order.
func (b *Bot) ConcatRaw() ([]byte, error) {
	var buf bytes.Buffer
	err := b.store().with(func() error {
		segs, err := b.store().segments()
		if err != nil {
			return err
		}
		for _, seg := range segs {
			raw, err := os.ReadFile(seg.path)
			if err != nil {
				return err
			}
			buf.Write(raw)
		}
		return nil
	})
	return buf.Bytes(), err
}

// SegmentFiles returns sealed and open segment paths in sequence order.
func (b *Bot) SegmentFiles() ([]string, error) {
	var out []string
	err := b.store().with(func() error {
		segs, err := b.store().segments()
		if err != nil {
			return err
		}
		for _, seg := range segs {
			out = append(out, seg.path)
		}
		return nil
	})
	return out, err
}

type segment struct {
	start int
	end   int
	path  string
	open  bool
}

func (s *segStore) segments() ([]segment, error) {
	matches, err := filepath.Glob(filepath.Join(s.transcriptDir(), "seg-*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []segment
	for _, path := range matches {
		base := filepath.Base(path)
		start, end, open, err := parseSegName(base)
		if err != nil {
			continue
		}
		out = append(out, segment{start: start, end: end, path: path, open: open})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out, nil
}

func parseSegName(base string) (start, end int, open bool, err error) {
	base = strings.TrimSuffix(base, ".jsonl")
	parts := strings.Split(base, "-")
	if len(parts) != 3 || parts[0] != "seg" {
		return 0, 0, false, fmt.Errorf("transcript: bad segment %s", base)
	}
	start, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false, err
	}
	if parts[2] == "open" {
		return start, 0, true, nil
	}
	end, err = strconv.Atoi(parts[2])
	return start, end, false, err
}

func startOf(path string) (int, error) {
	start, _, _, err := parseSegName(filepath.Base(path))
	return start, err
}

func (s *segStore) fileFor(start int) (string, error) {
	pat := filepath.Join(s.transcriptDir(), fmt.Sprintf("seg-%012d-*.jsonl", start))
	matches, err := filepath.Glob(pat)
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("transcript: segment %d resolved to %d files", start, len(matches))
	}
	return matches[0], nil
}

func spillBlob(dir string, e Entry, limit int64) Entry {
	if e.Message.Role != provider.RoleTool || e.Blob != "" {
		return e
	}
	if int64(len(e.Message.Content)) <= limit {
		return e
	}
	id, err := newBlobID()
	if err != nil {
		return e
	}
	path := filepath.Join(dir, "blobs", id)
	if err := writeSynced(path, []byte(e.Message.Content), 0600); err != nil {
		return e
	}
	preview, _ := clip.Bytes(e.Message.Content, blobPreview)
	e.Message.Content = preview
	e.Blob = id
	return e
}

func (s *segStore) indexBody(e Entry) string {
	if e.Blob == "" {
		return ftsBody(e)
	}
	raw, err := os.ReadFile(filepath.Join(s.transcriptDir(), "blobs", e.Blob))
	if err != nil {
		return ftsBody(e)
	}
	full := e
	full.Message.Content = string(raw)
	return ftsBody(full)
}

func writeSynced(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func newBlobID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func ftsBody(e Entry) string {
	var b strings.Builder
	b.WriteString(e.Message.Role)
	b.WriteByte(' ')
	b.WriteString(e.Message.Content)
	for _, tc := range e.Message.ToolCalls {
		b.WriteByte(' ')
		b.WriteString(tc.Function.Name)
		b.WriteByte(' ')
		b.WriteString(tc.Function.Arguments)
	}
	return b.String()
}

func ftsQuery(q string) string {
	fields := strings.Fields(q)
	var parts []string
	for _, f := range fields {
		f = strings.Map(func(r rune) rune {
			switch r {
			case '"', '\'', '*', ':', '(', ')', '^':
				return -1
			default:
				return r
			}
		}, f)
		if f == "" {
			continue
		}
		parts = append(parts, `"`+f+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

func marshalEntry(e Entry) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *segStore) syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func activeBlocks(blocks []SummaryBlock) []SummaryBlock {
	var out []SummaryBlock
	for i, b := range blocks {
		if b.From < 1 || b.To < b.From || strings.TrimSpace(b.Text) == "" {
			continue
		}
		superseded := false
		for j, o := range blocks {
			if i == j || o.From < 1 || o.To < o.From || strings.TrimSpace(o.Text) == "" {
				continue
			}
			wider := o.From <= b.From && o.To >= b.To && (o.To-o.From > b.To-b.From)
			sameLater := o.From == b.From && o.To == b.To && j > i
			if wider || sameLater {
				superseded = true
				break
			}
		}
		if !superseded {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}

func uncoveredSpans(n int, blocks []SummaryBlock) [][2]int {
	if n < 1 {
		return nil
	}
	type iv struct{ a, b int }
	var covered []iv
	for _, b := range activeBlocks(blocks) {
		a, z := b.From, b.To
		if z < 1 || a > n {
			continue
		}
		if a < 1 {
			a = 1
		}
		if z > n {
			z = n
		}
		covered = append(covered, iv{a, z})
	}
	sort.Slice(covered, func(i, j int) bool { return covered[i].a < covered[j].a })
	var merged []iv
	for _, c := range covered {
		if len(merged) == 0 || c.a > merged[len(merged)-1].b+1 {
			merged = append(merged, c)
			continue
		}
		if c.b > merged[len(merged)-1].b {
			merged[len(merged)-1].b = c.b
		}
	}
	var out [][2]int
	cur := 1
	for _, c := range merged {
		if cur < c.a {
			out = append(out, [2]int{cur, c.a - 1})
		}
		if c.b+1 > cur {
			cur = c.b + 1
		}
	}
	if cur <= n {
		out = append(out, [2]int{cur, n})
	}
	return out
}
