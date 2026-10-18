package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xbot/internal/peer"
)

const (
	peerPageDefault = 50
	peerPageMax     = 200
)

type peerRow struct {
	ID   string    `json:"id"`
	Seq  int       `json:"seq"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Text string    `json:"text"`
	TS   time.Time `json:"ts"`
	Kind string    `json:"kind,omitempty"`
}

func (s *Server) peerMessages(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	aName := r.PathValue("name")
	bName := r.PathValue("peer")
	if aName == "" || aName == bName {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	a, err := s.owned(aName)
	if err != nil {
		writeMissing(w, err)
		return
	}
	limit, err := peerLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad limit")
		return
	}
	var msgs []peer.Direct
	if strings.Contains(bName, "@") {
		// A bot on a paired device: both halves are in a's transcript.
		msgs, err = peer.RemoteConversation(a, bName)
		if errors.Is(err, peer.ErrBadRemote) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
	} else {
		b, err := s.owned(bName)
		if err != nil {
			writeMissing(w, err)
			return
		}
		msgs, err = peer.Conversation(a, b)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	page, err := peerPage(msgs, r.URL.Query().Get("before"), limit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad before")
		return
	}
	rows := make([]peerRow, 0, len(page))
	for _, m := range page {
		rows = append(rows, peerRow{
			ID: m.ID, Seq: m.Seq, From: m.From, To: m.To, Text: m.Text, TS: m.Time.UTC(), Kind: m.Kind,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": rows})
}

func peerLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return peerPageDefault, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > peerPageMax {
		return 0, errors.New("bad limit")
	}
	return n, nil
}

func peerPage(msgs []peer.Direct, before string, limit int) ([]peer.Direct, error) {
	end := len(msgs)
	if strings.TrimSpace(before) != "" {
		idx, err := peerCursor(msgs, before)
		if err != nil {
			return nil, err
		}
		end = idx
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return msgs[start:end], nil
}

// peerCursor finds the exclusive upper bound. before is "<to>:<seq>", or a bare
// seq when exactly one message in the thread carries that number.
func peerCursor(msgs []peer.Direct, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	// The seq never holds a colon; a device name in "bot@device" may.
	to, seqPart, qualified := raw, "", false
	if i := strings.LastIndexByte(raw, ':'); i >= 0 {
		to, seqPart, qualified = raw[:i], raw[i+1:], true
	}
	if !qualified {
		seq, err := strconv.Atoi(raw)
		if err != nil || seq < 1 {
			return 0, errors.New("bad before")
		}
		idx := -1
		for i, m := range msgs {
			if m.Seq != seq {
				continue
			}
			if idx >= 0 {
				return 0, errors.New("bad before")
			}
			idx = i
		}
		if idx < 0 {
			return 0, errors.New("bad before")
		}
		return idx, nil
	}
	seq, err := strconv.Atoi(seqPart)
	if err != nil || seq < 1 || to == "" {
		return 0, errors.New("bad before")
	}
	for i, m := range msgs {
		if m.To == to && m.Seq == seq {
			return i, nil
		}
	}
	return 0, errors.New("bad before")
}
