package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
)

// postCLIEvent reserves the existing handle before posting, so the event cannot
// be absorbed by another turn and the streamed outcome belongs to this wake.
func (s *Server) postCLIEvent(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Sender string `json:"sender"`
		Text   string `json:"text"`
	}
	if decode(r, &in) != nil || strings.TrimSpace(in.Sender) == "" || len(in.Sender) > 64 || strings.TrimSpace(in.Text) == "" || len(in.Text) > 16<<10 {
		writeErr(w, http.StatusBadRequest, "bad event")
		return
	}
	if !s.BeginTurn() {
		writeErr(w, http.StatusServiceUnavailable, "restarting")
		return
	}
	defer s.EndTurn()
	lock := s.waitHandle(r.Context(), b)
	if lock == nil {
		return
	}
	defer lock.Release()
	k, gate, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	gate.Lock()
	defer gate.Unlock()
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ev := link.NewEvent("cli", "event", in.Text, strings.TrimSpace(in.Sender), time.Now())
	if err = box.Post(ev); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	startStream(w)
	sendEvent(w, "accepted", map[string]string{"id": ev.ID})
	rs := &replyStream{w: w}
	k.Bot = b
	k.OnPart = rs.part
	k.OnDelta = rs.delta
	k.OnTool = rs.tool
	k.OnResult = rs.result
	var findings []string
	k.OnFinding = func(text string) { findings = append(findings, text) }
	prevApprove := k.Approve
	k.Approve = s.webApprove(b, k, rs.send)
	defer func() {
		k.OnPart = nil
		k.OnDelta = nil
		k.OnTool = nil
		k.OnResult = nil
		k.OnFinding = nil
		k.Approve = prevApprove
		rs.close()
	}()
	err = k.Handle(context.WithoutCancel(r.Context()), ev)
	ackErr := box.Ack(ev.ID)
	if err != nil {
		rs.send("error", map[string]string{"error": err.Error()})
		return
	}
	if ackErr != nil {
		rs.send("error", map[string]string{"error": ackErr.Error()})
		return
	}
	if k.HandedOff() {
		rs.send("error", map[string]string{"error": "event turn handed off; outcome unavailable"})
		return
	}
	visible := rs.reply()
	if len(findings) > 0 {
		if visible != "" {
			visible += "\n\n"
		}
		visible += strings.Join(findings, "\n\n")
	}
	rs.send("done", doneBody{Text: visible})
}
