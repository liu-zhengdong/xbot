package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/link"
)

type inboxItem struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Type       string    `json:"type"`
	Sender     string    `json:"sender"`
	SenderKind string    `json:"sender_kind,omitempty"`
	Time       time.Time `json:"time"`
	Summary    string    `json:"summary"`
	Status     string    `json:"status"`
}

var inboxFence = regexp.MustCompile(`(?i)</?untrusted_event\b[^>]*>`)

var inboxDelivery = regexp.MustCompile(`<delivery\b[^>]*>`)
var inboxWatch = regexp.MustCompile(`^<watch\b[^>]*>`)

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, http.StatusBadRequest, "bad limit")
			return
		}
		limit = n
	}
	events := map[string]link.Event{}
	acked := map[string]bool{}
	err = fsutil.WithLock(b.InboxPath()+".lock", func() error {
		return fsutil.ReadJSONL(b.InboxPath(), func(line []byte) error {
			var rec struct {
				Op    string      `json:"op"`
				Event *link.Event `json:"event"`
				ID    string      `json:"id"`
			}
			if err := json.Unmarshal(line, &rec); err != nil {
				return err
			}
			if rec.Op == "post" && rec.Event != nil {
				if _, ok := events[rec.Event.ID]; !ok {
					events[rec.Event.ID] = *rec.Event
				}
			}
			if rec.Op == "ack" {
				acked[rec.ID] = true
			}
			return nil
		})
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot read inbox")
		return
	}
	items := make([]inboxItem, 0, len(events))
	pending := 0
	for _, ev := range events {
		if ev.Source == link.NameWeb || ev.Source == link.NameTerminal || ev.Source == link.NameRemoteOwner || ev.Source == link.NameApproval {
			continue
		}
		status := "acked"
		if !acked[ev.ID] {
			status = "pending"
			pending++
		}
		summary := strings.TrimSpace(inboxFence.ReplaceAllString(ev.Content, ""))
		if strings.HasPrefix(ev.Source, "webhook:") || ev.Source == "webhook" {
			if loc := inboxDelivery.FindStringIndex(summary); loc != nil {
				summary = summary[loc[1]:]
				if end := strings.Index(summary, "</delivery>"); end >= 0 {
					summary = summary[:end]
				}
				summary = strings.TrimSpace(summary)
			}
		} else if strings.HasPrefix(ev.Source, "watch:") || ev.Source == "watch" {
			summary = strings.TrimSpace(inboxWatch.ReplaceAllString(summary, ""))
			summary = strings.TrimSpace(strings.TrimSuffix(summary, "</watch>"))
		}
		chars := []rune(summary)
		if len(chars) > 1000 {
			summary = string(chars[:1000]) + "…"
		}
		items = append(items, inboxItem{inboxText(ev.ID), inboxText(ev.Source), inboxText(ev.Type), inboxText(ev.Sender), inboxText(ev.SenderKind), ev.Time, summary, status})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Time.Equal(items[j].Time) {
			return items[i].ID > items[j].ID
		}
		return items[i].Time.After(items[j].Time)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending_count": pending, "items": items})
}

func inboxText(s string) string {
	chars := []rune(s)
	if len(chars) > 128 {
		return string(chars[:128])
	}
	return s
}
