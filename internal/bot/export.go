package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"xbot/internal/provider"
)

// TranscriptExport selects a slice of one bot's append-only transcript.
// From and To are inclusive sequence numbers. Zero means the whole transcript.
// Tools includes tool calls and tool-role lines. Loc formats markdown timestamps;
// nil uses the process local zone, which is the owner's machine.
type TranscriptExport struct {
	Format string
	Tools  bool
	From   int
	To     int
	Loc    *time.Location
	// Redact overrides RedactText for this call. Nil uses RedactText.
	Redact func(string) string
}

type exportEntry struct {
	Seq     int              `json:"seq"`
	Time    time.Time        `json:"time"`
	EventID string           `json:"event_id,omitempty"`
	Message provider.Message `json:"message"`
}

type exportSummary struct {
	Kind  string    `json:"kind"`
	From  int       `json:"from"`
	To    int       `json:"to"`
	Text  string    `json:"text"`
	Focus string    `json:"focus,omitempty"`
	At    time.Time `json:"at,omitempty"`
}

// ExportTranscript renders md for a person or jsonl for re-import.
// jsonl entry lines unmarshal into Entry; seq is extra and ignored on import.
// Lines with kind "summary" are summary blocks, not transcript rows.
// Each written piece passes through RedactText.
func ExportTranscript(b *Bot, opt TranscriptExport) (string, error) {
	var buf bytes.Buffer
	if err := ExportTo(&buf, b, opt); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}

// ValidateExport checks format and range before a response starts.
func ValidateExport(b *Bot, opt TranscriptExport) error {
	n, err := b.Count()
	if err != nil {
		return err
	}
	_, _, _, err = exportBounds(n, opt)
	return err
}

// ExportTo streams the transcript one segment at a time. Tool blobs are loaded
// for the lines that are written. The caller sees a finished line or section
// only after that piece has been redacted.
func ExportTo(w io.Writer, b *Bot, opt TranscriptExport) error {
	n, err := b.Count()
	if err != nil {
		return err
	}
	format, from, to, err := exportSpan(n, opt)
	if err != nil {
		return err
	}
	st, err := b.LoadState()
	if err != nil {
		return err
	}
	loc := opt.Loc
	if loc == nil {
		loc = time.Local
	}
	redact := opt.Redact
	if redact == nil {
		redact = RedactText
	}
	write := func(s string) error {
		_, err := io.WriteString(w, redact(s))
		return err
	}
	var blocks []SummaryBlock
	for _, block := range st.Blocks {
		if block.To < from || block.From > to {
			continue
		}
		blocks = append(blocks, block)
	}
	if format == "jsonl" {
		return exportJSONLTo(b, blocks, from, to, opt.Tools, write)
	}
	if err := write(fmt.Sprintf("# %s\n", b.Name())); err != nil {
		return err
	}
	for _, block := range blocks {
		if err := write(summaryMD(block, loc)); err != nil {
			return err
		}
	}
	if n == 0 || from > to {
		return nil
	}
	return b.Walk(from, to, func(seq int, e Entry) error {
		e, err := b.Hydrate(e)
		if err != nil {
			return err
		}
		msg, ok := visibleMessage(e.Message, opt.Tools)
		if !ok {
			return nil
		}
		return write(entryMD(seq, e.Time, msg, opt.Tools, loc))
	})
}

func exportSpan(n int, opt TranscriptExport) (format string, from, to int, err error) {
	format, from, to, err = exportBounds(n, opt)
	return format, from, to, err
}

func exportBounds(n int, opt TranscriptExport) (string, int, int, error) {
	format := strings.TrimSpace(opt.Format)
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "jsonl" {
		return "", 0, 0, fmt.Errorf("export: unknown format %q", opt.Format)
	}
	from, to := 1, n
	if opt.From != 0 || opt.To != 0 {
		if opt.From == 0 || opt.To == 0 {
			return "", 0, 0, fmt.Errorf("export: from and to are set together")
		}
		if n == 0 || opt.From < 1 || opt.To < opt.From || opt.To > n {
			return "", 0, 0, fmt.Errorf("export: range %d-%d outside 1-%d", opt.From, opt.To, n)
		}
		from, to = opt.From, opt.To
	}
	if n == 0 {
		from, to = 1, 0
	}
	return format, from, to, nil
}

func exportJSONLTo(b *Bot, blocks []SummaryBlock, from, to int, tools bool, write func(string) error) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	emit := func(v any) error {
		buf.Reset()
		if err := enc.Encode(v); err != nil {
			return err
		}
		return write(buf.String())
	}
	for _, block := range blocks {
		if err := emit(exportSummary{
			Kind: "summary", From: block.From, To: block.To, Text: block.Text, Focus: block.Focus, At: block.At,
		}); err != nil {
			return err
		}
	}
	if to < from {
		return nil
	}
	return b.Walk(from, to, func(seq int, e Entry) error {
		e, err := b.Hydrate(e)
		if err != nil {
			return err
		}
		msg, ok := visibleMessage(e.Message, tools)
		if !ok {
			return nil
		}
		return emit(exportEntry{Seq: seq, Time: e.Time, EventID: e.EventID, Message: msg})
	})
}

func summaryMD(block SummaryBlock, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## Summary seq %d-%d\n", block.From, block.To)
	if strings.TrimSpace(block.Focus) != "" {
		fmt.Fprintf(&b, "focus: %s\n", strings.TrimSpace(block.Focus))
	}
	if !block.At.IsZero() {
		fmt.Fprintf(&b, "%s\n", block.At.In(loc).Format("2006-01-02 15:04:05 MST"))
	}
	if text := strings.TrimSpace(block.Text); text != "" {
		b.WriteString("\n")
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

func entryMD(seq int, at time.Time, msg provider.Message, tools bool, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n## seq %d · %s · %s\n\n", seq, stamp(at, loc), msg.Role)
	if msg.Content != "" {
		b.WriteString(msg.Content)
		if !strings.HasSuffix(msg.Content, "\n") {
			b.WriteByte('\n')
		}
	}
	if tools {
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(&b, "tool %s %s\n", tc.Function.Name, tc.Function.Arguments)
		}
	}
	return b.String()
}

func visibleMessage(m provider.Message, tools bool) (provider.Message, bool) {
	if !tools {
		if m.Role == provider.RoleTool {
			return provider.Message{}, false
		}
		if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) > 0 {
			return provider.Message{}, false
		}
		m.ToolCalls = nil
		return m, true
	}
	return m, true
}

func stamp(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format("2006-01-02 15:04:05 MST")
}
