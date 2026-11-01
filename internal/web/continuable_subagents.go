package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	textclip "xbot/internal/clip"
	"xbot/internal/exec"
	"xbot/internal/pipe/limits"
	"xbot/internal/provider"
)

// Child controls use the executor's owner routing, independently of the Bot's
// foreground gate. In particular, a serve.Pass Kernel may still own the child.
func (s *Server) messageSubagent(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct {
		Text     string `json:"text"`
		Delivery string `json:"delivery,omitempty"`
	}
	if err := decodeSubagentControl(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	k, _, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, bot.RedactText(err.Error()))
		return
	}
	task, err := k.MessageSubagent(r.Context(), r.PathValue("id"), in.Text, in.Delivery)
	if err != nil {
		writeSubagentError(w, err)
		return
	}
	writeSubagentReceipt(w, task)
}

func (s *Server) stopSubagent(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	var in struct{}
	if err := decodeSubagentControl(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	k, _, err := s.kernel(b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, bot.RedactText(err.Error()))
		return
	}
	task, err := k.StopSubagent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeSubagentError(w, err)
		return
	}
	writeSubagentReceipt(w, task)
}

func decodeSubagentControl(r *http.Request, v any) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil {
		return err
	}
	if len(raw) > 64<<10 {
		return errors.New("request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("bad request")
	}
	return nil
}

func writeSubagentReceipt(w http.ResponseWriter, task exec.Task) {
	writeJSON(w, http.StatusOK, struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Position int    `json:"position,omitempty"`
		Blocked  bool   `json:"blocked,omitempty"`
		TurnSeq  int    `json:"turn_seq,omitempty"`
	}{task.ID, task.Status, task.Position, task.Blocked, task.TurnSeq})
}

func writeSubagentError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, os.ErrNotExist):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, exec.ErrNotContinuable), errors.Is(err, exec.ErrActiveElsewhere):
		code = http.StatusConflict
	case errors.Is(err, exec.ErrQueueFull):
		code = http.StatusTooManyRequests
	}
	writeErr(w, code, bot.RedactText(err.Error()))
}

type subagentEntry struct {
	Seq        int              `json:"seq"`
	Time       time.Time        `json:"time"`
	Message    provider.Message `json:"message"`
	Kind       string           `json:"kind,omitempty"`
	TurnSeq    int              `json:"turn_seq,omitempty"`
	Sender     string           `json:"sender,omitempty"`
	InputSeq   int              `json:"input_seq,omitempty"`
	Delivery   string           `json:"delivery,omitempty"`
	Status     string           `json:"status,omitempty"`
	ResultPath string           `json:"result_path,omitempty"`
	Internal   bool             `json:"internal,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
}

type subagentTranscript struct {
	ID          string          `json:"id"`
	Entries     []subagentEntry `json:"entries"`
	HasMore     bool            `json:"has_more"`
	NextBefore  int             `json:"next_before,omitempty"`
	TurnSeq     int             `json:"turn_seq"`
	Status      string          `json:"status"`
	Continuable bool            `json:"continuable"`
}

func (s *Server) subagentTranscript(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	id := r.PathValue("id")
	// Read paths are selected only from the authenticated Bot and request ID,
	// never from the contents of a task record.
	if !strings.HasPrefix(id, "sa_") || !bot.ValidName(id) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	before, count := 0, 40
	for key, dest := range map[string]*int{"before": &before, "limit": &count} {
		if raw, ok := r.URL.Query()[key]; ok {
			if len(raw) != 1 {
				writeErr(w, http.StatusBadRequest, "bad "+key)
				return
			}
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 || key == "limit" && n > 200 {
				writeErr(w, http.StatusBadRequest, "bad "+key)
				return
			}
			*dest = n
		}
	}
	dir := b.AgentDir(id)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, bot.RedactText(err.Error()))
		return
	}
	meta, err := exec.ReadMeta(dir)
	if errors.Is(err, os.ErrNotExist) || err == nil && meta.ID != id {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, bot.RedactText(err.Error()))
		return
	}
	window, err := exec.ReadTranscript(dir, before, count)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, bot.RedactText(err.Error()))
		return
	}
	_, descriptorErr := exec.ReadDescriptor(dir)
	out := subagentTranscript{ID: id, Entries: make([]subagentEntry, 0, len(window.Entries)),
		HasMore: window.HasMore, NextBefore: window.NextBefore, TurnSeq: meta.TurnSeq,
		Status: meta.Status, Continuable: descriptorErr == nil}
	if active, ok := exec.ActiveTask(dir); ok {
		out.Status, out.TurnSeq = active.Status, active.TurnSeq
	}
	envelope, _ := json.Marshal(out)
	totalBytes := len(envelope) + 256 // cursor digits, separators, and final newline
	sizes := make([]int, 0, len(window.Entries))
	for _, entry := range window.Entries {
		m := entry.Message
		parts := make([]provider.ContentPart, 0, len(m.Content))
		for _, p := range m.Content {
			if p.Type == provider.PartReasoning {
				continue
			}
			p.VendorRaw = nil
			parts = append(parts, p)
		}
		m.Content = parts
		m = m.MapText(bot.RedactText)
		row := subagentEntry{Seq: entry.Seq, Time: entry.Time, Message: m,
			Kind: entry.Kind, TurnSeq: entry.TurnSeq, InputSeq: entry.InputSeq,
			Delivery: entry.Delivery, Sender: entry.Sender, Status: entry.Status,
			ResultPath: bot.RedactText(entry.ResultPath), Internal: entry.IsInternal(), Truncated: entry.Truncated}
		raw, _ := json.Marshal(row)
		if len(raw) > 64<<10 {
			preview := m.PortableText()
			for _, call := range m.Calls() {
				preview += "\n" + call.Function.Name + " " + call.Function.Arguments
			}
			preview, _ = textclip.Bytes(preview, 8192)
			row.Message = provider.Message{Role: m.Role, Content: provider.TextContent(preview)}
			row.Truncated = true
			raw, _ = json.Marshal(row)
		}
		out.Entries = append(out.Entries, row)
		sizes = append(sizes, len(raw)+1)
		totalBytes += len(raw) + 1
	}
	// Trim only the older edge of a tail/before page. The retained first seq is
	// the next exclusive cursor, including when one large row was previewed.
	lo := 0
	for totalBytes > limits.MaxBody && len(out.Entries)-lo > 1 {
		totalBytes -= sizes[lo]
		lo++
		out.HasMore = true
	}
	out.Entries = out.Entries[lo:]
	if out.HasMore && len(out.Entries) > 0 {
		out.NextBefore = out.Entries[0].Seq
	} else {
		out.NextBefore = 0
	}
	writeJSON(w, http.StatusOK, out)
}
