package exec

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/provider"
)

// Descriptor contains only facts needed to assemble a new local activation.
// Credentials and runtime endpoints are deliberately absent.
type Descriptor struct {
	Version         int      `json:"version"`
	ID              string   `json:"id"`
	Bot             string   `json:"bot"`
	Type            string   `json:"type"`
	Mode            string   `json:"mode"`
	CWD             string   `json:"cwd"`
	DeviceID        string   `json:"device_id,omitempty"`
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	Thinking        string   `json:"thinking,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	Tools           []string `json:"tools"`
	MaxSteps        int      `json:"max_steps"`
	TimeoutSec      int      `json:"timeout_sec"`
}

func ReadDescriptor(dir string) (Descriptor, error) {
	b, err := os.ReadFile(filepath.Join(dir, "descriptor.json"))
	if os.IsNotExist(err) {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return Descriptor{}, ErrNotFound
		}
		return Descriptor{}, ErrNotContinuable
	}
	if err != nil {
		return Descriptor{}, err
	}
	var d Descriptor
	if err := json.Unmarshal(b, &d); err != nil {
		return Descriptor{}, err
	}
	if d.Version != 1 || !idRe.MatchString(d.ID) || d.ID != filepath.Base(filepath.Clean(dir)) || (d.Mode != ModeBrief && d.Mode != ModeFork) {
		return Descriptor{}, fmt.Errorf("exec: invalid descriptor")
	}
	return d, nil
}

func writeDescriptor(dir string, d Descriptor) error {
	b, err := fsutil.Marshal(d)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(dir, "descriptor.json"), b, 0600)
}

// SeqEntry extends the existing bot entry; Seq is its physical JSONL line.
// InputSeq refers to the single message_received record holding the input text.
type SeqEntry struct {
	bot.Entry
	Seq        int    `json:"seq,omitempty"`
	Kind       string `json:"kind,omitempty"`
	TurnSeq    int    `json:"turn_seq,omitempty"`
	InputSeq   int    `json:"input_seq,omitempty"`
	Delivery   string `json:"delivery,omitempty"`
	Sender     string `json:"sender,omitempty"`
	Status     string `json:"status,omitempty"`
	ResultPath string `json:"result_path,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type TranscriptWindow struct {
	Entries    []SeqEntry `json:"entries"`
	HasMore    bool       `json:"has_more"`
	NextBefore int        `json:"next_before,omitempty"`
}

// scanTranscript streams complete lines and ignores an unfinished append tail.
func scanTranscript(dir string, visit func(SeqEntry) error) error {
	f, err := os.Open(transcriptPath(dir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for seq := 1; ; seq++ {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var e SeqEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return fmt.Errorf("exec: transcript line %d: %w", seq, err)
		}
		e.Seq = seq
		if err := visit(e); err != nil {
			return err
		}
	}
}

// ReadTranscript returns an ascending bounded tail window before an exclusive seq.
func ReadTranscript(dir string, before, limit int) (TranscriptWindow, error) {
	if before < 0 || limit < 0 {
		return TranscriptWindow{}, fmt.Errorf("exec: negative transcript window")
	}
	if limit == 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}
	w := TranscriptWindow{Entries: make([]SeqEntry, 0, limit)}
	f, err := os.Open(transcriptPath(dir))
	if os.IsNotExist(err) {
		return w, nil
	}
	if err != nil {
		return w, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	count := 0
	for seq := 1; before == 0 || seq < before; seq++ {
		// The public window never allocates an unbounded line. Original data
		// remains available through the transcript/expand paths.
		const lineCap = 64 << 10
		var line []byte
		truncated := false
		for {
			part, readErr := r.ReadSlice('\n')
			if !truncated {
				if len(line)+len(part) <= lineCap {
					line = append(line, part...)
				} else {
					line = nil
					truncated = true
				}
			}
			if readErr == bufio.ErrBufferFull {
				continue
			}
			if readErr == io.EOF {
				goto complete
			}
			if readErr != nil {
				return w, readErr
			}
			break
		}
		e := SeqEntry{Seq: seq, Truncated: truncated}
		if truncated {
			e.Kind = "truncated"
			e.Entry = bot.Entry{Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent("记录过长；请通过 \"$XBOT_BIN\" subagent expand --self 按此序号读取原文。")}}
		} else if err := json.Unmarshal(line, &e); err != nil {
			return w, fmt.Errorf("exec: transcript line %d: %w", seq, err)
		}
		e.Seq = seq
		count++
		if len(w.Entries) == limit {
			copy(w.Entries, w.Entries[1:])
			w.Entries = w.Entries[:limit-1]
		}
		w.Entries = append(w.Entries, e)
	}
complete:
	w.HasMore = count > len(w.Entries)
	if w.HasMore && len(w.Entries) > 0 {
		w.NextBefore = w.Entries[0].Seq
	}
	return w, nil
}

func readRecords(dir string) ([]SeqEntry, error) {
	var entries []SeqEntry
	err := scanTranscript(dir, func(e SeqEntry) error { entries = append(entries, e); return nil })
	return entries, err
}

func (j *job) recordCount() int {
	j.recordMu.Lock()
	defer j.recordMu.Unlock()
	return j.recordSeq
}

func (j *job) record(e SeqEntry) (seq int, err error) {
	err = j.withStorageIO(func() error { var e2 error; seq, e2 = j.recordLocked(e); return e2 })
	return
}

func (j *job) recordLocked(e SeqEntry) (int, error) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.Seq = 0
	if err := fsutil.AppendJSONL(transcriptPath(j.dir), e); err != nil {
		return 0, err
	}
	j.recordSeq++
	return j.recordSeq, nil
}

func (j *job) readRecords() (entries []SeqEntry, err error) {
	err = j.withStorageIO(func() error { var e error; entries, e = readRecords(j.dir); return e })
	return
}

func (j *job) appendLegacy(e bot.Entry) error {
	return j.withStorageIO(func() error { return appendEntry(j.dir, e) })
}

func inputMessage(e SeqEntry) provider.Message {
	m := e.Message
	prefix := ""
	if e.Sender == "owner" {
		prefix = "来自用户的消息：\n"
	} else if len(e.Sender) > 4 && e.Sender[:4] == "bot:" {
		prefix = "来自 Bot " + e.Sender[4:] + " 的消息：\n"
	}
	if prefix != "" {
		m.Content = provider.TextContent(prefix + m.Text())
	}
	return m
}

func projectRecords(entries []SeqEntry) []provider.Message {
	inputs := make(map[int]SeqEntry)
	var msgs []provider.Message
	for _, e := range entries {
		switch e.Kind {
		case "message_received":
			inputs[e.Seq] = e
		case "message_consumed":
			if input, ok := inputs[e.InputSeq]; ok {
				msgs = append(msgs, inputMessage(input))
			}
		case "":
			if e.Message.Role != "" {
				msgs = append(msgs, e.Message)
			}
		}
	}
	return msgs
}

// sealCalls appends cancellation tombstones; it never executes historical calls.
func (j *job) sealCalls() error { return j.withStorageIO(j.sealCallsLocked) }

func (j *job) sealCallsLocked() error {
	entries, err := readRecords(j.dir)
	if err != nil {
		return err
	}
	var open []provider.ToolCall
	for _, m := range projectRecords(entries) {
		for _, tc := range m.Calls() {
			open = append(open, tc)
		}
		if m.Role == provider.RoleTool {
			for i := range open {
				if open[i].ID == m.CallID() {
					open = append(open[:i], open[i+1:]...)
					break
				}
			}
		}
	}
	for _, tc := range open {
		m := provider.Message{Role: provider.RoleTool, Name: tc.Function.Name, Content: provider.ToolResult(tc.ID, "工具调用已中断；没有自动重新执行。")}
		if _, err := j.recordLocked(SeqEntry{Entry: bot.Entry{Message: m, Origin: bot.OriginInternal}, TurnSeq: j.turnSeq}); err != nil {
			return err
		}
	}
	return nil
}
