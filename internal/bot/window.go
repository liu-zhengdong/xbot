package bot

import (
	"database/sql"
	"errors"
	"fmt"
)

const DefaultWindowLimit = 60
const MaxWindowLimit = 200
const windowBytes = 128 << 10

var ErrWindowTarget = errors.New("transcript target does not exist")

// WindowQuery selects a contiguous transcript range. Cursors are exclusive;
// Around includes the target. With no cursor, the window ends at the tail.
type WindowQuery struct {
	Before, After, Around, Limit int
}

type WindowMeta struct {
	HeadSeq       int  `json:"head_seq"`
	TailSeq       int  `json:"tail_seq"`
	Total         int  `json:"total"`
	NextBefore    int  `json:"next_before"`
	NextAfter     int  `json:"next_after"`
	HasMoreBefore bool `json:"has_more_before"`
	HasMoreAfter  bool `json:"has_more_after"`
}

type TranscriptWindow struct {
	Rows []ReplayRow
	WindowMeta
}

// Window owns cursor arithmetic, byte/count limits and legacy branch replay.
// Only selected indexed offsets are read; history is never loaded then sliced.
func (b *Bot) Window(q WindowQuery) (TranscriptWindow, error) {
	var out TranscriptWindow
	if q.Limit == 0 {
		q.Limit = DefaultWindowLimit
	}
	cursors := 0
	for _, n := range []int{q.Before, q.After, q.Around} {
		if n < 0 {
			return out, fmt.Errorf("negative transcript cursor")
		}
		if n > 0 {
			cursors++
		}
	}
	if cursors > 1 || q.Limit < 1 || q.Limit > MaxWindowLimit {
		return out, fmt.Errorf("invalid transcript window")
	}
	s := b.store()
	err := s.with(func() error {
		n, err := s.maxSeq()
		if err != nil {
			return err
		}
		out.Total = n
		if q.Around > n {
			return ErrWindowTarget
		}
		if n == 0 || q.Before == 1 || q.After >= n && q.After > 0 {
			return nil
		}
		from, to := max(1, n-q.Limit+1), n
		switch {
		case q.Before > 0:
			to = min(n, q.Before-1)
			from = max(1, to-q.Limit+1)
		case q.After > 0:
			from = q.After + 1
			to = min(n, q.After+min(q.Limit, n-q.After))
		case q.Around > 0:
			from = max(1, q.Around-q.Limit/2)
			to = min(n, from+min(q.Limit-1, n-from))
			from = max(1, to-q.Limit+1)
		}
		rows, err := s.db.Query(`SELECT seq, len FROM lines WHERE seq BETWEEN ? AND ? ORDER BY seq`, from, to)
		if err != nil {
			return err
		}
		type size struct{ seq, bytes int }
		var sizes []size
		for rows.Next() {
			var v size
			if err := rows.Scan(&v.seq, &v.bytes); err != nil {
				rows.Close()
				return err
			}
			sizes = append(sizes, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(sizes) == 0 {
			return err
		}
		// Grow from the requested edge/target. A single oversized record must
		// still advance the cursor; the HTTP projection gives it a preview.
		anchor := len(sizes) - 1
		if q.After > 0 {
			anchor = 0
		} else if q.Around > 0 {
			anchor = q.Around - from
		}
		lo, hi, used := anchor, anchor, sizes[anchor].bytes
		for lo > 0 || hi < len(sizes)-1 {
			next := hi + 1
			if lo > 0 && (hi == len(sizes)-1 || anchor-lo <= hi-anchor) {
				next = lo - 1
			}
			if used+sizes[next].bytes > windowBytes {
				break
			}
			used += sizes[next].bytes
			lo, hi = min(lo, next), max(hi, next)
		}
		out.HeadSeq, out.TailSeq = sizes[lo].seq, sizes[hi].seq
		entries, err := s.slice(out.HeadSeq, out.TailSeq)
		if err != nil {
			return err
		}
		seqs := make([]SeqEntry, 0, len(entries))
		for i, e := range entries {
			seqs = append(seqs, SeqEntry{Seq: out.HeadSeq + i, Entry: e})
		}
		// A fork marker can retrospectively assign rows preceding the window.
		// The small derived interval index avoids reading all future rows.
		spans, err := s.db.Query(`SELECT seq, from_seq, branch FROM fork_spans WHERE seq > ? AND from_seq < ? ORDER BY seq`, out.TailSeq, out.TailSeq)
		if err != nil {
			return err
		}
		for spans.Next() {
			var seq, start int
			var branch string
			if err := spans.Scan(&seq, &start, &branch); err != nil {
				spans.Close()
				return err
			}
			seqs = append(seqs, SeqEntry{Seq: seq, Entry: Entry{Mark: &ForkMark{Kind: ForkKindStart, From: start, Branch: branch}}})
		}
		err = spans.Err()
		spans.Close()
		if err != nil {
			return err
		}
		replay, _ := Replay(seqs)
		out.Rows = replay[:len(entries)]
		out.NextBefore, out.NextAfter = out.HeadSeq, out.TailSeq
		out.HasMoreBefore, out.HasMoreAfter = out.HeadSeq > 1, out.TailSeq < n
		return nil
	})
	return out, err
}

// ToolResult reads one indexed result, including when it lies outside a window.
func (b *Bot) ToolResult(id string) (Entry, bool, error) {
	var entry Entry
	found := false
	s := b.store()
	err := s.with(func() error {
		var seq int
		err := s.db.QueryRow(`SELECT seq FROM tool_results WHERE id=?`, id).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		entries, err := s.slice(seq, seq)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			entry, found = entries[0], true
		}
		return nil
	})
	return entry, found, err
}
