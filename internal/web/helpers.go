package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/notify"
)

func queryBool(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, errors.New("bad tools")
	}
}

func queryRange(fromRaw, toRaw string) (int, int, error) {
	fromRaw, toRaw = strings.TrimSpace(fromRaw), strings.TrimSpace(toRaw)
	if fromRaw == "" && toRaw == "" {
		return 0, 0, nil
	}
	if fromRaw == "" || toRaw == "" {
		return 0, 0, errors.New("from and to are set together")
	}
	from, err := strconv.Atoi(fromRaw)
	if err != nil {
		return 0, 0, errors.New("bad from")
	}
	to, err := strconv.Atoi(toRaw)
	if err != nil {
		return 0, 0, errors.New("bad to")
	}
	return from, to, nil
}

func noteCount(b *bot.Bot) (int, error) {
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		return 0, err
	}
	return len(notes), nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func writeMissing(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, bot.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}
