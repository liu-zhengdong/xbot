package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	textclip "xbot/internal/clip"
)

func transcriptQuery(r *http.Request) (bot.WindowQuery, error) {
	q := bot.WindowQuery{}
	cursors := 0
	for key, dest := range map[string]*int{"before": &q.Before, "after": &q.After, "around": &q.Around, "limit": &q.Limit} {
		values, ok := r.URL.Query()[key]
		if !ok {
			continue
		}
		if len(values) != 1 {
			return q, fmt.Errorf("bad %s", key)
		}
		n, err := strconv.Atoi(values[0])
		if err != nil || n < 1 || key == "limit" && n > bot.MaxWindowLimit {
			return q, fmt.Errorf("bad %s", key)
		}
		*dest = n
		if key != "limit" {
			cursors++
		}
	}
	if cursors > 1 {
		return q, fmt.Errorf("choose one transcript cursor")
	}
	return q, nil
}

func (s *Server) searchTranscript(w http.ResponseWriter, r *http.Request) {
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
	if raw, ok := r.URL.Query()["limit"]; ok {
		if len(raw) != 1 {
			writeErr(w, http.StatusBadRequest, "bad limit")
			return
		}
		limit, err = strconv.Atoi(raw[0])
	}
	if err != nil || limit < 1 || limit > 100 || len(query) > 1024 {
		writeErr(w, http.StatusBadRequest, "bad search query or limit")
		return
	}
	seqs, err := b.Search(query, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type hit struct {
		Seq     int       `json:"seq"`
		Preview string    `json:"preview"`
		Time    time.Time `json:"time"`
	}
	hits := make([]hit, 0, len(seqs))
	for _, seq := range seqs {
		part, err := b.Slice(seq, seq)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		preview := ""
		if len(part) > 0 {
			preview, _ = textclip.Bytes(part[0].Message.PortableText(), 240)
		}
		var at time.Time
		if len(part) > 0 {
			at = part[0].Time
		}
		hits = append(hits, hit{Seq: seq, Preview: preview, Time: at})
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": hits})
}
