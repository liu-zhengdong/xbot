package bot

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"xbot/internal/provider"
)

const DefaultWindowLimit = 60
const MaxWindowLimit = 200
const MaxVisibleWindowLimit = 400
const windowBytes = 256 << 10
const defaultVisibleTarget = 25

var ErrWindowTarget = errors.New("transcript target does not exist")

// WindowQuery selects a contiguous transcript range. Cursors are exclusive;
// Around includes the target. With no cursor, the window ends at the tail.
// An omitted Limit grows tail/before windows to 25 visible deliveries, within
// MaxVisibleWindowLimit and windowBytes. Explicit limits remain raw-record limits.
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
	visibleWindow := q.Limit == 0 && q.After == 0 && q.Around == 0
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
		if visibleWindow {
			from = max(1, to-MaxVisibleWindowLimit+1)
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
			if visibleWindow && hi-lo+1 >= q.Limit {
				break
			}
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
		entries, err := s.slice(sizes[lo].seq, sizes[hi].seq)
		if err != nil {
			return err
		}
		for visibleWindow {
			counts, err := s.windowVisibleCounts(entries)
			if err != nil {
				return err
			}
			visible, keep := 0, len(entries)
			for i := len(entries) - 1; i >= 0; i-- {
				visible += counts[i]
				if visible >= defaultVisibleTarget && len(entries)-i >= q.Limit {
					keep = len(entries) - i
					break
				}
			}
			if visible >= defaultVisibleTarget {
				lo += len(entries) - keep
				entries = entries[len(entries)-keep:]
				break
			}
			oldLo := lo
			for lo > 0 && oldLo-lo < DefaultWindowLimit && used+sizes[lo-1].bytes <= windowBytes {
				lo--
				used += sizes[lo].bytes
			}
			if lo == oldLo {
				break
			}
			older, err := s.slice(sizes[lo].seq, sizes[oldLo-1].seq)
			if err != nil {
				return err
			}
			entries = append(older, entries...)
		}
		out.HeadSeq, out.TailSeq = sizes[lo].seq, sizes[hi].seq
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
		var err error
		entry, found, err = s.toolResult(id)
		return err
	})
	return entry, found, err
}

func (s *segStore) toolResult(id string) (Entry, bool, error) {
	var seq int
	err := s.db.QueryRow(`SELECT seq FROM tool_results WHERE id=?`, id).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	entries, err := s.slice(seq, seq)
	if err != nil || len(entries) == 0 {
		return Entry{}, false, err
	}
	return entries[0], true, nil
}

var windowEvent = regexp.MustCompile(`^<untrusted_event\s+[^>]*>\n[\s\S]*\n</untrusted_event>$`)
var toolChatSince = time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)

// Mirror spokenLines/skipMessage's delivery units, not raw transcript rows:
// non-internal user inputs close a run; landed says or attachments suppress
// legacy prose in that run. Source event lines and cards are not chat bubbles.
// This runs under the store lock and reuses the result index across windows.
func (s *segStore) windowVisibleCounts(entries []Entry) ([]int, error) {
	counts := make([]int, len(entries))
	var run []int
	closeRun := func() error {
		says, mediaLine, lastSay := 0, -1, -1
		for _, i := range run {
			e := entries[i]
			if len(e.Images)+len(e.Audios) > 0 {
				mediaLine = i
			}
			for _, call := range e.Message.Calls() {
				if call.Function.Name != "send_message_to_user" && call.Function.Name != "notify_owner" {
					continue
				}
				result, found, err := s.toolResult(call.ID)
				if err != nil {
					return err
				}
				text := result.Message.Text()
				if !found || !(strings.HasPrefix(text, "sent to the owner's chat") || strings.Contains(text, "notified the owner")) {
					continue
				}
				says++
				lastSay = i
				var args struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				if text := strings.TrimSpace(args.Text); text != "" && text != "SILENT" && !e.IsInternal() && (strings.TrimSpace(e.Message.Text()) != "SILENT" || len(e.Images)+len(e.Audios) > 0) {
					counts[i]++
				}
			}
		}
		if mediaLine >= 0 {
			// spokenLines queues the run's media on its last delivered say.
			if lastSay >= 0 {
				mediaLine = lastSay
			}
			e := entries[mediaLine]
			if !e.IsInternal() && (strings.TrimSpace(e.Message.Text()) != "SILENT" || len(e.Images)+len(e.Audios) > 0) {
				counts[mediaLine]++
			}
		}
		if says == 0 && mediaLine < 0 {
			for _, i := range run {
				e := entries[i]
				text := strings.TrimSpace(e.Message.Text())
				parts, visibleParts := 0, false
				for _, p := range e.Message.Content {
					if p.Type == provider.PartHosted || p.Type == provider.PartCitation {
						parts++
						visibleParts = visibleParts || p.Hosted == nil || p.Hosted.Name != "image_generation"
					}
				}
				if !e.IsInternal() && e.Time.Before(toolChatSince) && text != "SILENT" && (text != "" || visibleParts || len(e.Message.Calls()) == 0 && parts == 0) {
					counts[i]++
				}
			}
		}
		run = nil
		return nil
	}
	for i, e := range entries {
		if e.Mark != nil {
			continue
		}
		switch e.Message.Role {
		case provider.RoleAssistant:
			run = append(run, i)
		case provider.RoleUser:
			if e.IsInternal() {
				continue
			}
			if err := closeRun(); err != nil {
				return nil, err
			}
			text := e.Message.Text()
			if !windowEvent.MatchString(text) && !IsApprovalCard(text) && !strings.HasPrefix(text, "<coding_card ") {
				counts[i]++
			}
		}
	}
	if err := closeRun(); err != nil {
		return nil, err
	}
	return counts, nil
}
