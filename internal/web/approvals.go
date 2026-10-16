package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/machine"
	"xbot/internal/provider"
	"xbot/internal/review"
)

func (s *Server) approvalWait() time.Duration {
	if s.ApprovalWait > 0 {
		return s.ApprovalWait
	}
	return 60 * time.Second
}

// webApprove shows one confirmation card in the stream and persists it in the
// transcript, first as pending and then with the resolved state, so the card
// survives the reply ending and a page reload. A timeout or cancelled wake
// with no decision records an expired card, never a pending one.
func (s *Server) webApprove(b *bot.Bot, k *kernel.Kernel, send func(string, any)) func(context.Context, string, string) (bool, error) {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		id := newApprovalID()
		ch := make(chan approvalDecision, 1)
		key := b.Name() + "/" + id
		s.mu.Lock()
		if s.approvals == nil {
			s.approvals = map[string]chan approvalDecision{}
		}
		s.approvals[key] = ch
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.approvals, key)
			s.mu.Unlock()
		}()
		title, subtitle := review.CardText(tool, agent.ToolArgs(ctx), summary, machine.Local)
		appendCard := func(state string) {
			_ = b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: bot.ApprovalCardText(id, tool, title, subtitle, summary, state)}})
		}
		appendCard(bot.ApprovalPending)
		payload := map[string]any{"id": id, "tool": tool, "summary": summary, "title": title, "subtitle": subtitle}
		names := approvalSecretNames(tool, summary)
		if len(names) > 0 {
			payload["secrets"] = names
		}
		send("approval", payload)
		timer := time.NewTimer(s.approvalWait())
		defer timer.Stop()
		select {
		case d := <-ch:
			if d.allow && k != nil && len(names) > 0 && len(d.secrets) > 0 {
				k.StageSecrets(linkIDFromSummary(summary), filterSecrets(names, d.secrets))
			}
			state := bot.ApprovalDeclined
			if d.allow {
				state = bot.ApprovalApproved
			}
			appendCard(state)
			return d.allow, nil
		case <-timer.C:
			appendCard(bot.ApprovalExpired)
			return false, kernel.ErrApprovalExpired
		case <-ctx.Done():
			appendCard(bot.ApprovalExpired)
			return false, kernel.ErrApprovalExpired
		}
	}
}

func approvalSecretNames(tool, summary string) []string {
	if tool != "links" {
		return nil
	}
	return secretNamesFromSummary(summary)
}

func linkIDFromSummary(summary string) string {
	line, _, _ := strings.Cut(summary, "\n")
	line = strings.TrimSpace(line)
	id, ok := strings.CutPrefix(line, "install ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

func secretNamesFromSummary(summary string) []string {
	var names []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "secrets:")
		if !ok {
			continue
		}
		for _, part := range strings.Split(rest, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				names = append(names, part)
			}
		}
	}
	return names
}

func filterSecrets(names []string, values map[string]string) map[string]string {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	out := map[string]string{}
	for name, value := range values {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if allowed[name] && value != "" {
			out[name] = value
		}
	}
	return out
}

func newApprovalID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Allow   bool              `json:"allow"`
		Secrets map[string]string `json:"secrets"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	key := b.Name() + "/" + r.PathValue("id")
	s.mu.Lock()
	ch := s.approvals[key]
	s.mu.Unlock()
	if ch == nil {
		writeErr(w, http.StatusNotFound, "no such approval")
		return
	}
	select {
	case ch <- approvalDecision{allow: in.Allow, secrets: in.Secrets}:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeErr(w, http.StatusConflict, "already decided")
	}
}
