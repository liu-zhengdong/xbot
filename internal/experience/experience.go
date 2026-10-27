// Package experience is a bot's log of what happened: short entries the agent
// records itself, each anchored to a transcript range. It is a SQLite file in
// the bot directory, separate from the always-loaded memory folder.
package experience

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// FileName is the database inside a bot directory.
const FileName = "experience.sqlite"

// Entry is one recorded experience. From and To are transcript sequence
// numbers; zero means not anchored. Segment is a short digest of that range.
type Entry struct {
	ID        int64
	CreatedAt time.Time
	Summary   string
	Focus     string
	From      int
	To        int
	Segment   string
}

// Store is the experience database of one bot directory. Each call opens and
// closes its own connection, so many bots and many tests hold no handles.
type Store struct{ path string }

// At is the store for a bot directory. Nothing is created until Add.
func At(botDir string) Store { return Store{path: filepath.Join(botDir, FileName)} }

func (s Store) open() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+s.path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS experiences (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  summary TEXT NOT NULL,
  focus TEXT NOT NULL DEFAULT '',
  from_seq INTEGER,
  to_seq INTEGER,
  segment TEXT NOT NULL DEFAULT ''
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// exists reports whether there is anything to read, so a reader never
// creates an empty database.
func (s Store) exists() (bool, error) {
	_, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Add stores e and returns it with its id.
func (s Store) Add(e Entry) (Entry, error) {
	db, err := s.open()
	if err != nil {
		return e, err
	}
	defer db.Close()
	res, err := db.Exec(`INSERT INTO experiences(created_at, summary, focus, from_seq, to_seq, segment) VALUES(?,?,?,?,?,?)`,
		e.CreatedAt.UTC().Format(time.RFC3339Nano), e.Summary, e.Focus, nullSeq(e.From), nullSeq(e.To), e.Segment)
	if err != nil {
		return e, err
	}
	e.ID, err = res.LastInsertId()
	return e, err
}

// Latest is the newest entry and the total count. ok is false when empty.
func (s Store) Latest() (e Entry, count int, ok bool, err error) {
	list, count, err := s.recent(1)
	if err != nil || len(list) == 0 {
		return Entry{}, count, false, err
	}
	return list[0], count, true, nil
}

// Recent returns up to limit entries, newest first.
func (s Store) Recent(limit int) ([]Entry, error) {
	list, _, err := s.recent(limit)
	return list, err
}

// Get returns one entry by id.
func (s Store) Get(id int64) (Entry, bool, error) {
	if ok, err := s.exists(); !ok || err != nil {
		return Entry{}, false, err
	}
	db, err := s.open()
	if err != nil {
		return Entry{}, false, err
	}
	defer db.Close()
	rows, err := db.Query(selectCols+` WHERE id=?`, id)
	if err != nil {
		return Entry{}, false, err
	}
	list, err := scan(rows)
	if err != nil || len(list) == 0 {
		return Entry{}, false, err
	}
	return list[0], true, nil
}

const selectCols = `SELECT id, created_at, summary, focus, from_seq, to_seq, segment FROM experiences`

func (s Store) recent(limit int) ([]Entry, int, error) {
	if ok, err := s.exists(); !ok || err != nil {
		return nil, 0, err
	}
	db, err := s.open()
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM experiences`).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(selectCols+` ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	list, err := scan(rows)
	return list, count, err
}

func scan(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var at string
		var from, to sql.NullInt64
		if err := rows.Scan(&e.ID, &at, &e.Summary, &e.Focus, &from, &to, &e.Segment); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, at)
		e.From, e.To = int(from.Int64), int(to.Int64)
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullSeq(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}
