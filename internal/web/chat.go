package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xbot/internal/bot"
	textclip "xbot/internal/clip"
	"xbot/internal/pipe/limits"
	"xbot/internal/provider"
)

type chatAPIRow struct {
	bot.ChatRow
	FromDevice *apiFromDevice `json:"from_device,omitempty"`
	Relay      *apiRelay      `json:"relay,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	q, err := transcriptQuery(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	window, err := b.Chat().Window(q)
	if err != nil {
		code := 500
		if errors.Is(err, bot.ErrWindowTarget) {
			code = 404
		}
		writeErr(w, code, err.Error())
		return
	}
	// Chat IDs and snapshots change only at owner-visible delivery boundaries.
	// Tool noise is deliberately absent from this validator.
	stamp := fmt.Sprintf("chat-%d-%d", window.Generation, window.Window.Total)
	if fi, err := os.Stat(filepath.Join(s.Root, "remote", "devices.json")); err == nil {
		stamp += fmt.Sprintf(":%d:%d", fi.Size(), fi.ModTime().UnixNano())
	}
	etag := fmt.Sprintf(`W/"%s:%d:%d:%d:%d"`, stamp, q.Before, q.After, q.Around, q.Limit)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	src := loadSources(s.Root)
	rows := make([]chatAPIRow, 0, len(window.Messages))
	sizes := []int{}
	total := 0
	for _, m := range window.Messages {
		// Only public presentation parts belong in the owner-visible snapshot.
		m.Meta.Parts = publicParts(provider.Message{Content: m.Meta.Parts})
		row := chatAPIRow{ChatRow: m}
		if m.Kind == "owner" {
			row.FromDevice = src.fromDevice(m.Meta.EventID)
		}
		if m.Kind == "source" {
			row.Relay = src.relay(m.Text)
		}
		raw, _ := json.Marshal(row)
		if len(raw) > 64<<10 {
			row.Text, _ = textclip.Bytes(row.Text, 8192)
			row.Meta.Parts = nil
			row.Truncated = true
			raw, _ = json.Marshal(row)
		}
		rows = append(rows, row)
		sizes = append(sizes, len(raw)+1)
		total += len(raw) + 1
	}
	lo, hi := 0, len(rows)
	for total > limits.MaxBody-(60<<10) && hi-lo > 1 {
		if q.After > 0 || q.Around > 0 && q.Around-rows[lo].ID < rows[hi-1].ID-q.Around {
			hi--
			total -= sizes[hi]
		} else {
			total -= sizes[lo]
			lo++
		}
	}
	rows = rows[lo:hi]
	if len(rows) > 0 {
		window.Messages = window.Messages[lo:hi]
		window.SetBounds()
	}
	if !window.Window.HasMoreAfter && q.Before == 0 && q.Around == 0 {
		_ = s.writeCursor(b.Name(), window.TranscriptTotal)
	}
	writeJSON(w, 200, map[string]any{"messages": rows, "window": window.Window})
}
func (s *Server) searchChat(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 20
	if values, ok := r.URL.Query()["limit"]; ok {
		if len(values) != 1 {
			writeErr(w, 400, "bad limit")
			return
		}
		limit, err = strconv.Atoi(values[0])
	}
	if err != nil || limit < 1 || limit > 100 || len(query) > 1024 {
		writeErr(w, 400, "bad search query or limit")
		return
	}
	hits, err := b.Chat().Search(query, limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"matches": hits})
}
