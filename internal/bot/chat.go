package bot

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"xbot/internal/provider"
)

const chatGeneration = 1
const DefaultChatLimit = 40

// Chat is the owner-visible delivery timeline. Transcript remains the ledger;
// this disposable projection has no editing API.
type Chat struct{ b *Bot }

func (b *Bot) Chat() *Chat { return &Chat{b: b} }

type ChatRow struct {
	ID            int       `json:"id"`
	TranscriptSeq int       `json:"transcript_seq"`
	DeliveryKey   string    `json:"delivery_key"`
	Kind          string    `json:"kind"`
	Time          time.Time `json:"time"`
	Text          string    `json:"text"`
	Images        []string  `json:"images,omitempty"`
	Audios        []string  `json:"audios,omitempty"`
	ReplyToSeq    int       `json:"reply_to_seq,omitempty"`
	ReplyToID     int       `json:"reply_to_id,omitempty"`
	Meta          ChatMeta  `json:"meta,omitempty"`
}
type ChatMeta struct {
	EventID string                 `json:"event_id,omitempty"`
	Branch  string                 `json:"branch,omitempty"`
	Parts   []provider.ContentPart `json:"parts,omitempty"`
}
type ChatWindowMeta struct {
	HeadID        int  `json:"head_id"`
	TailID        int  `json:"tail_id"`
	Total         int  `json:"total"`
	NextBefore    int  `json:"next_before"`
	NextAfter     int  `json:"next_after"`
	HasMoreBefore bool `json:"has_more_before"`
	HasMoreAfter  bool `json:"has_more_after"`
}
type ChatWindow struct {
	Messages        []ChatRow      `json:"messages"`
	Window          ChatWindowMeta `json:"window"`
	TranscriptTotal int            `json:"-"`
	Generation      int            `json:"-"`
}

func initChat(db *sql.DB) error {
	_, err := db.Exec(`
 CREATE TABLE IF NOT EXISTS chat (id INTEGER PRIMARY KEY, delivery_key TEXT NOT NULL UNIQUE, transcript_seq INTEGER NOT NULL, snapshot TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS chat_seq ON chat(transcript_seq);
 CREATE VIRTUAL TABLE IF NOT EXISTS chat_fts USING fts5(body, tokenize='unicode61');
 CREATE TABLE IF NOT EXISTS chat_projection (singleton INTEGER PRIMARY KEY CHECK(singleton=1), generation INTEGER NOT NULL, state TEXT NOT NULL);
 `)
	return err
}

// Record is the only insertion seam, shared by live delivery and backfill.
// An existing delivery key never changes its text or attachments.
func (c *Chat) Record(row ChatRow) error {
	if row.DeliveryKey == "" || row.TranscriptSeq < 1 {
		return fmt.Errorf("invalid chat delivery")
	}
	s := c.b.store()
	return s.with(func() error {
		if err := c.sync(s); err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := c.record(tx, row); err != nil {
			return err
		}
		return tx.Commit()
	})
}
func (c *Chat) record(tx *sql.Tx, row ChatRow) error {
	row.ID = 0
	row.Time = row.Time.UTC()
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO chat(delivery_key,transcript_seq,snapshot) VALUES(?,?,?) ON CONFLICT(delivery_key) DO NOTHING`, row.DeliveryKey, row.TranscriptSeq, string(raw))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	text := row.Text
	if m := chatEvent.FindStringSubmatch(text); m != nil {
		text = m[2]
	}
	_, err = tx.Exec(`INSERT INTO chat_fts(rowid,body) VALUES(?,?)`, id, chatFTSText(text))
	return err
}

type chatProjection struct {
	Seq       int
	Pending   []SeqEntry
	Landed    map[string]bool
	Completed bool
}

var chatEvent = regexp.MustCompile(`^<untrusted_event\s+([^>]*)>\n([\s\S]*)\n</untrusted_event>$`)
var chatAttr = regexp.MustCompile(`([\w-]+)="([^"]*)"`)

// chatInput is the single visibility predicate for owner, card and source rows.
// Assistant visibility is determined for the whole run in deliveries below.
func chatInput(seq int, e Entry) (ChatRow, bool) {
	row := ChatRow{TranscriptSeq: seq, Time: e.Time, Text: e.Message.Text(), Images: e.Images, Audios: e.Audios, ReplyToSeq: e.ReplyTo,
		Meta: ChatMeta{EventID: e.EventID, Branch: e.Branch}}
	if e.Mark != nil || e.IsInternal() || e.Message.Role != provider.RoleUser {
		return row, false
	}
	row.Kind, row.DeliveryKey = "owner", fmt.Sprintf("user:%d", seq)
	if card, ok := ParseApprovalCard(row.Text); ok {
		row.Kind, row.DeliveryKey = "card", "card:"+card.ID+":"+card.State
	} else if strings.HasPrefix(row.Text, "<coding_card ") {
		attrs := chatAttributes(strings.SplitN(row.Text, "\n", 2)[0])
		row.Kind, row.DeliveryKey = "card", "card:"+attrs["id"]+":"+attrs["status"]
	} else if m := chatEvent.FindStringSubmatch(row.Text); m != nil {
		attrs := chatAttributes(m[1])
		source := attrs["source"]
		if source == "shell" || source == "timer" || source == "subagent" && attrs["type"] == "fork_end" {
			return row, false
		}
		row.Kind, row.DeliveryKey = "source", fmt.Sprintf("source:%d", seq)
		// The peer API owns peer originals. A source row keeps only its display
		// snapshot, not another copy of the peer conversation.
		if strings.HasPrefix(source, "bot:") || strings.HasPrefix(source, "group:") {
			body := []rune(m[2])
			if len(body) > 80 {
				body = body[:80]
			}
			row.Text = "<untrusted_event " + m[1] + ">\n" + string(body) + "\n</untrusted_event>"
		}
	}
	return row, true
}

// Han characters are indexed as a phrase of individual tokens, so short
// Chinese keywords work without loading snapshots to search them.
func chatFTSText(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			out.WriteByte(' ')
			out.WriteRune(r)
			out.WriteByte(' ')
		} else {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

func chatAttributes(s string) map[string]string {
	out := map[string]string{}
	for _, m := range chatAttr.FindAllStringSubmatch(s, -1) {
		out[m[1]] = m[2]
	}
	return out
}
func chatSay(name string) bool { return name == "send_message_to_user" || name == "notify_owner" }
func chatLanded(text string) bool {
	return strings.HasPrefix(text, "sent to the owner's chat") || strings.Contains(text, "notified the owner")
}

// deliveries freezes one completed run. Media from the whole run is attached
// to its last landed say, never back-written into an earlier snapshot.
func (p *chatProjection) deliveries() []ChatRow {
	var says, legacy []ChatRow
	var images, audios []string
	reply := 0
	for _, se := range p.Pending {
		e := se.Entry
		images = append(images, e.Images...)
		audios = append(audios, e.Audios...)
		if e.ReplyTo > 0 {
			reply = e.ReplyTo
		}
		for _, call := range e.Message.Calls() {
			if !chatSay(call.Function.Name) || !p.Landed[call.ID] {
				continue
			}
			var args struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
			says = append(says, ChatRow{TranscriptSeq: se.Seq, DeliveryKey: "say:" + call.ID, Kind: "bot", Time: e.Time, Text: strings.TrimSpace(args.Text), Meta: ChatMeta{Branch: e.Branch}})
		}
		text := strings.TrimSpace(e.Message.Text())
		var parts []provider.ContentPart
		for _, part := range e.Message.Content {
			if part.Type == provider.PartHosted || part.Type == provider.PartCitation {
				part.VendorRaw = nil
				parts = append(parts, part)
			}
		}
		visibleParts := false
		for _, part := range parts {
			visibleParts = visibleParts || part.Hosted == nil || part.Hosted.Name != "image_generation"
		}
		if e.Time.Before(toolChatSince) && text != "SILENT" && (text != "" || visibleParts || len(e.Message.Calls()) == 0 && len(parts) == 0) {
			legacy = append(legacy, ChatRow{TranscriptSeq: se.Seq, DeliveryKey: fmt.Sprintf("legacy:%d", se.Seq), Kind: "bot", Time: e.Time, Text: text, ReplyToSeq: e.ReplyTo, Meta: ChatMeta{Branch: e.Branch, Parts: parts}})
		}
	}
	if len(says) == 0 && len(images)+len(audios) > 0 && len(p.Pending) > 0 {
		se := p.Pending[len(p.Pending)-1]
		says = append(says, ChatRow{TranscriptSeq: se.Seq, DeliveryKey: fmt.Sprintf("media:%d", se.Seq), Kind: "bot", Time: se.Entry.Time})
	}
	if len(says) > 0 {
		says[0].ReplyToSeq = reply
		says[len(says)-1].Images = images
		says[len(says)-1].Audios = audios
		filtered := says[:0]
		for _, row := range says {
			if row.Text != "" && row.Text != "SILENT" || len(row.Images)+len(row.Audios) > 0 {
				filtered = append(filtered, row)
			}
		}
		return filtered
	}
	return legacy
}

func (c *Chat) sync(s *segStore) error {
	if err := initChat(s.db); err != nil {
		return err
	}
	var version int
	var raw string
	err := s.db.QueryRow(`SELECT generation,state FROM chat_projection WHERE singleton=1`).Scan(&version, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	p := chatProjection{Landed: map[string]bool{}}
	backfill := version != chatGeneration
	if !backfill {
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
	}
	n, err := s.maxSeq()
	if err != nil {
		return err
	}
	if !backfill && p.Seq == n {
		return nil
	}
	if backfill {
		if _, err := s.db.Exec(`DELETE FROM chat; DELETE FROM chat_fts`); err != nil {
			return err
		}
	}
	var additions []ChatRow
	flush := func(branch *string) {
		finished := chatProjection{Landed: p.Landed}
		var pending []SeqEntry
		for _, se := range p.Pending {
			if branch == nil || se.Entry.Branch == *branch {
				finished.Pending = append(finished.Pending, se)
			} else {
				pending = append(pending, se)
			}
		}
		additions = append(additions, finished.deliveries()...)
		for _, se := range finished.Pending {
			for _, call := range se.Entry.Message.Calls() {
				delete(p.Landed, call.ID)
			}
		}
		p.Pending = pending
	}
	// Commit snapshots and the replay checkpoint together. A crash either
	// preserves both or replays the same delivery keys; it cannot lose a run.
	save := func(generation int) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, row := range additions {
			if err := c.record(tx, row); err != nil {
				return err
			}
		}
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO chat_projection VALUES(1,?,?) ON CONFLICT(singleton) DO UPDATE SET generation=excluded.generation,state=excluded.state`, generation, string(data)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		additions = nil
		return nil
	}
	for from := p.Seq + 1; from <= n; from += 128 {
		entries, err := s.slice(from, min(n, from+127))
		if err != nil {
			return err
		}
		for i, e := range entries {
			seq := from + i
			if e.Mark == nil && !e.IsInternal() && e.Message.Role == provider.RoleUser {
				if backfill {
					flush(nil)
					p.Completed = false
				}
				if row, ok := chatInput(seq, e); ok {
					additions = append(additions, row)
				}
			}
			if e.Mark == nil && !e.IsInternal() && e.Message.Role == provider.RoleAssistant {
				keep := e.Time.Before(toolChatSince) || len(e.Images)+len(e.Audios) > 0
				for _, call := range e.Message.Calls() {
					keep = keep || chatSay(call.Function.Name)
				}
				if keep {
					p.Pending = append(p.Pending, SeqEntry{Seq: seq, Entry: e})
				}
				p.Completed = len(e.Message.Calls()) == 0
				if !backfill && p.Completed {
					flush(&e.Branch)
				}
			}
			if e.Message.Role == provider.RoleTool && chatLanded(e.Message.Text()) {
				p.Landed[e.Message.CallID()] = true
			}
			p.Seq = seq
		}
		generation := chatGeneration
		if backfill {
			generation = 0
		}
		if err := save(generation); err != nil {
			return err
		}
	}
	if backfill {
		if p.Completed {
			flush(nil)
		}
		return save(chatGeneration)
	}
	return nil
}

func (c *Chat) Window(q WindowQuery) (ChatWindow, error) {
	out := ChatWindow{Messages: []ChatRow{}, Generation: chatGeneration}
	if q.Limit == 0 {
		q.Limit = DefaultChatLimit
	}
	cursors := 0
	for _, n := range []int{q.Before, q.After, q.Around} {
		if n < 0 {
			return out, fmt.Errorf("negative chat cursor")
		}
		if n > 0 {
			cursors++
		}
	}
	if cursors > 1 || q.Limit < 1 || q.Limit > MaxWindowLimit {
		return out, fmt.Errorf("invalid chat window")
	}
	s := c.b.store()
	err := s.with(func() error {
		if err := c.sync(s); err != nil {
			return err
		}
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM chat`).Scan(&out.Window.Total); err != nil {
			return err
		}
		var err error
		out.TranscriptTotal, err = s.maxSeq()
		if err != nil {
			return err
		}
		n := out.Window.Total
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
			to = min(n, q.After+min(q.Limit, n-min(n, q.After)))
		case q.Around > 0:
			from = max(1, q.Around-q.Limit/2)
			to = min(n, from+min(q.Limit-1, n-from))
			from = max(1, to-q.Limit+1)
		}
		rows, err := s.db.Query(`SELECT id,snapshot FROM chat WHERE id BETWEEN ? AND ? ORDER BY id`, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanChatRow(rows)
			if err != nil {
				return err
			}
			out.Messages = append(out.Messages, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for i := range out.Messages {
			m := &out.Messages[i]
			if m.ReplyToSeq == 0 {
				continue
			}
			err := s.db.QueryRow(`SELECT id FROM chat WHERE transcript_seq=? ORDER BY id LIMIT 1`, m.ReplyToSeq).Scan(&m.ReplyToID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if len(out.Messages) > 0 {
			out.SetBounds()
		}
		return nil
	})
	return out, err
}
func (w *ChatWindow) SetBounds() {
	m := &w.Window
	m.HeadID = w.Messages[0].ID
	m.TailID = w.Messages[len(w.Messages)-1].ID
	m.NextBefore, m.NextAfter = m.HeadID, m.TailID
	m.HasMoreBefore, m.HasMoreAfter = m.HeadID > 1, m.TailID < m.Total
}
func scanChatRow(rows *sql.Rows) (ChatRow, error) {
	var row ChatRow
	var id int
	var raw string
	if err := rows.Scan(&id, &raw); err != nil {
		return row, err
	}
	err := json.Unmarshal([]byte(raw), &row)
	row.ID = id
	return row, err
}

type ChatHit struct {
	ID            int       `json:"id"`
	TranscriptSeq int       `json:"transcript_seq"`
	Preview       string    `json:"preview"`
	Time          time.Time `json:"time"`
	Kind          string    `json:"kind"`
}

func (c *Chat) Search(query string, limit int) ([]ChatHit, error) {
	hits := []ChatHit{}
	if limit < 1 || limit > 100 || len(query) > 1024 {
		return hits, fmt.Errorf("invalid chat search")
	}
	s := c.b.store()
	err := s.with(func() error {
		if err := c.sync(s); err != nil {
			return err
		}
		var terms []string
		for _, term := range strings.Fields(query) {
			term = strings.ReplaceAll(term, `"`, "")
			if term != "" {
				terms = append(terms, `"`+chatFTSText(term)+`"`)
			}
		}
		q := strings.Join(terms, " OR ")
		if q == "" {
			return nil
		}
		rows, err := s.db.Query(`SELECT chat.id,chat.snapshot FROM chat_fts JOIN chat ON chat.id=chat_fts.rowid WHERE chat_fts MATCH ? ORDER BY bm25(chat_fts) LIMIT ?`, q, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanChatRow(rows)
			if err != nil {
				return err
			}
			preview := []rune(row.Text)
			if len(preview) > 240 {
				preview = preview[:240]
			}
			hits = append(hits, ChatHit{ID: row.ID, TranscriptSeq: row.TranscriptSeq, Preview: string(preview), Time: row.Time, Kind: row.Kind})
		}
		return rows.Err()
	})
	return hits, err
}
