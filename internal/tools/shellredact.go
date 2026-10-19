package tools

import (
	"bytes"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
)

// This file is the streaming redaction engine used by the shell job writer. It
// knows nothing about job records: it takes a Redact function and a list of
// secret forms, and commits only the part of the stream that cannot still be
// the beginning of a secret. shelljob.go owns the job lifecycle.

// redactSpan is one exact replacement inside a raw buffer. start and end are
// raw byte offsets. repl is the already-final redaction of raw[start:end].
type redactSpan struct {
	start int
	end   int
	repl  string
}

type redactNeedle struct {
	old  string
	repl string
	pi   []int
}

// jobWriter appends redacted output. A suffix of pending stays in memory only
// when it is a prefix of a known secret form, so a secret split across writes
// is redacted before any of it is committed. Anything that cannot start a
// secret is committed as soon as its redaction is complete. Close redacts
// whatever remains. Committed bytes are not rewritten, so the stream length
// never shrinks. written is that committed length; discarded is the prefix
// rotated off the file.
type jobWriter struct {
	mu          sync.Mutex
	f           *os.File
	max         int
	redact      func(string) string
	forms       func() []string
	formKey     uint64
	formNeedles []redactNeedle // longest first, then bytes
	formIndex   *needleIndex
	formSet     bool
	needles     []redactNeedle // learned; kept in the same order
	needleIndex *needleIndex
	pending     []byte
	produced    int // committed redacted bytes, monotonic
	written     int // high-water stream end; equals produced
	discarded   int
	fileStable  int // committed bytes currently in the file
}

func newJobWriter(f *os.File, max int, redact func(string) string) *jobWriter {
	if max <= 0 {
		max = defaultJobBytes
	}
	return &jobWriter{f: f, max: max, redact: redact}
}

func (w *jobWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	if err := w.flushBlock(false); err != nil {
		return len(p), err
	}
	return len(p), nil
}

// Flush commits the safe redacted prefix. A suffix that might still begin a
// secret stays in memory and is not written.
func (w *jobWriter) Flush() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushBlock(false)
}

// prefixFunc is the Knuth–Morris–Pratt border table. pi[i] is the longest
// proper prefix of form[:i+1] that is also a suffix of it.
func prefixFunc(form string) []int {
	pi := make([]int, len(form))
	for i := 1; i < len(form); i++ {
		j := pi[i-1]
		for j > 0 && form[i] != form[j] {
			j = pi[j-1]
		}
		if form[i] == form[j] {
			j++
		}
		pi[i] = j
	}
	return pi
}

// prefixSuffixLen is the longest k such that raw ends with form[:k]. The
// border table makes one pass over the tail. A full copy of form at the end
// counts: that secret may still grow into a longer form.
func prefixSuffixLen(raw, form string, pi []int) int {
	if form == "" || raw == "" || len(pi) != len(form) {
		return 0
	}
	if len(raw) > len(form) {
		raw = raw[len(raw)-len(form):]
	}
	j := 0
	for i := 0; i < len(raw); i++ {
		for j > 0 && raw[i] != form[j] {
			j = pi[j-1]
		}
		if raw[i] == form[j] {
			j++
		}
		if j == len(form) && i+1 < len(raw) {
			j = pi[j-1]
		}
	}
	return j
}

// retreatCut moves cut back to the start of any form that straddles it.
// The caller has already held the longest prefix-suffix; this catches a
// completed form whose last byte is the first byte of that suffix.
func retreatCut(raw string, forms []redactNeedle, cut int) int {
	if cut <= 0 || cut > len(raw) {
		return cut
	}
	changed := true
	for changed {
		changed = false
		for _, form := range forms {
			old := form.old
			if old == "" || len(old) > len(raw) {
				continue
			}
			from := cut - len(old) + 1
			if from < 0 {
				from = 0
			}
			to := cut + len(old) - 1
			if to > len(raw) {
				to = len(raw)
			}
			seg := raw[from:to]
			off := 0
			for off < len(seg) {
				i := strings.Index(seg[off:], old)
				if i < 0 {
					break
				}
				abs := from + off + i
				if abs < cut && abs+len(old) > cut {
					cut = abs
					changed = true
					break
				}
				off += i + 1
			}
		}
	}
	if cut < 0 {
		return 0
	}
	return cut
}

func (w *jobWriter) hold(raw string) int {
	best := 0
	for _, n := range w.cachedFormNeedles() {
		k := prefixSuffixLen(raw, n.old, n.pi)
		if k > best {
			best = k
		}
	}
	return best
}

func (w *jobWriter) formList() []string {
	if w == nil || w.forms == nil {
		return nil
	}
	return w.forms()
}

// flushBlock appends the final-redacted prefix of pending. final also commits
// the held tail. Bytes already committed are not rewritten.
func (w *jobWriter) flushBlock(final bool) error {
	if w.f == nil {
		return nil
	}
	raw := w.pending
	if len(raw) == 0 {
		return nil
	}
	hold := 0
	if !final {
		s := string(raw)
		hold = w.hold(s)
		if len(raw) <= hold {
			return nil
		}
		// A form can share its last byte with the held suffix. That byte is
		// already inside the suffix hold, so the match is not a span yet, and
		// committing the earlier bytes would freeze a redaction the finished
		// form would have written differently.
		cut := retreatCut(s, w.cachedFormNeedles(), len(raw)-hold)
		hold = len(raw) - cut
		if len(raw) <= hold {
			return nil
		}
	}
	cut := len(raw) - hold
	red, spans, same, ok := w.redactSpans(string(raw))
	var rawEnd int
	var out []byte
	switch {
	case final && (!ok || same):
		rawEnd = len(raw)
		if same {
			out = raw
		} else {
			out = []byte(red)
		}
	case final:
		rawEnd = len(raw)
		out = []byte(projectSpans(string(raw), spans, rawEnd))
		if string(out) != red {
			out = []byte(red)
		}
	case same:
		rawEnd = cut
		out = raw[:rawEnd]
	case !ok:
		// Spans could not be proved. Leave every raw byte off disk until Close.
		return nil
	default:
		rawEnd = safeRawEnd(spans, cut)
		if !spansFit(spans, rawEnd) {
			return nil
		}
		projected := projectSpans(string(raw), spans, rawEnd)
		if !strings.HasPrefix(red, projected) {
			return nil
		}
		out = []byte(projected)
	}
	if rawEnd <= 0 {
		return nil
	}
	if err := w.writeCommitted(out); err != nil {
		return err
	}
	if rawEnd >= len(raw) {
		w.pending = nil
	} else {
		w.pending = append([]byte(nil), raw[rawEnd:]...)
	}
	return nil
}

// redactSpans redacts raw and returns the replacement spans that produced the
// result. same means no byte changed. ok is false when the spans could not be
// proved to match the redactor; the caller then commits nothing incomplete.
func (w *jobWriter) redactSpans(raw string) (red string, spans []redactSpan, same, ok bool) {
	if w == nil || w.redact == nil || raw == "" {
		return raw, nil, true, true
	}
	red = w.redact(raw)
	if red == raw {
		return red, nil, true, true
	}
	if spanned, hit := coverNeedles(raw, w.cachedFormNeedles(), w.formIndex); hit && projectSpans(raw, spanned, len(raw)) == red {
		return red, spanned, false, true
	}
	if spanned, hit := coverNeedles(raw, w.needles, w.needleIndex); hit && projectSpans(raw, spanned, len(raw)) == red {
		return red, spanned, false, true
	}
	learned, learnedOK := learnSpans(raw, red, w.redact)
	if !learnedOK || projectSpans(raw, learned, len(raw)) != red {
		return red, nil, false, false
	}
	for _, sp := range learned {
		w.remember(raw[sp.start:sp.end], sp.repl)
	}
	return red, learned, false, true
}

// cachedFormNeedles is the longest-first replacement list for Forms.
// A projection that does not match the redactor is ignored by the caller.
func (w *jobWriter) cachedFormNeedles() []redactNeedle {
	forms := w.formList()
	key := formsKey(forms)
	if w.formSet && key == w.formKey {
		return w.formNeedles
	}
	needles := make([]redactNeedle, 0, len(forms))
	for _, form := range forms {
		if form == "" || w.redact == nil {
			continue
		}
		repl := w.redact(form)
		if repl == form {
			continue
		}
		needles = append(needles, redactNeedle{old: form, repl: repl, pi: prefixFunc(form)})
	}
	sortNeedles(needles)
	w.formKey = key
	w.formNeedles = needles
	w.formIndex = indexNeedles(needles)
	w.formSet = true
	return needles
}

// needleIndex lists, for each first byte, the positions of the needles that
// start with it, in list order.
type needleIndex [256][]int32

func indexNeedles(list []redactNeedle) *needleIndex {
	var idx needleIndex
	for i, n := range list {
		if n.old != "" {
			idx[n.old[0]] = append(idx[n.old[0]], int32(i))
		}
	}
	return &idx
}

// sortNeedles orders longest first, then by bytes: at one position the longest
// form wins, as in the vault's single-pass replacer.
func sortNeedles(list []redactNeedle) {
	sort.SliceStable(list, func(i, j int) bool {
		if len(list[i].old) != len(list[j].old) {
			return len(list[i].old) > len(list[j].old)
		}
		return list[i].old < list[j].old
	})
}

func formsKey(forms []string) uint64 {
	var h uint64 = 14695981039346656037
	for _, f := range forms {
		h ^= uint64(len(f))
		h *= 1099511628211
		for i := 0; i < len(f); i++ {
			h ^= uint64(f[i])
			h *= 1099511628211
		}
	}
	h ^= uint64(len(forms))
	return h
}

func (w *jobWriter) remember(old, repl string) {
	if old == "" || old == repl {
		return
	}
	for _, n := range w.needles {
		if n.old == old {
			return
		}
	}
	w.needles = append(w.needles, redactNeedle{old: old, repl: repl})
	sortNeedles(w.needles)
	w.needleIndex = indexNeedles(w.needles)
}

// writeCommitted appends bytes that are already final-redacted. The stream
// length only moves forward.
func (w *jobWriter) writeCommitted(p []byte) error {
	if w.f == nil || len(p) == 0 {
		return nil
	}
	if _, err := w.f.Write(p); err != nil {
		return err
	}
	w.produced += len(p)
	w.fileStable += len(p)
	if w.produced > w.written {
		w.written = w.produced
	}
	return w.rotateStable()
}

// rotateStable drops the head of the committed bytes once the file is past the
// cap. Discarded bytes stay in the stream length; written does not shrink.
func (w *jobWriter) rotateStable() error {
	if w.f == nil || w.fileStable <= w.max {
		return nil
	}
	slack := w.max / 4
	if w.max < 4096 {
		slack = 0
	}
	if w.fileStable <= w.max+slack {
		return nil
	}
	keep := w.max
	if keep > w.fileStable {
		keep = w.fileStable
	}
	drop := w.fileStable - keep
	buf := make([]byte, keep)
	if _, err := w.f.ReadAt(buf, int64(drop)); err != nil {
		return err
	}
	if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
		drop += i + 1
		buf = buf[i+1:]
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := w.f.Truncate(0); err != nil {
		return err
	}
	if _, err := w.f.Write(buf); err != nil {
		return err
	}
	w.discarded += drop
	w.fileStable = len(buf)
	return nil
}

// safeRawEnd is the greatest raw offset at or before cut that is not strictly
// inside a replacement. A secret that crosses the retain window stays pending.
func safeRawEnd(spans []redactSpan, cut int) int {
	end := cut
	if end < 0 {
		return 0
	}
	changed := true
	for changed {
		changed = false
		for _, sp := range spans {
			if sp.start < end && end < sp.end {
				end = sp.start
				changed = true
			}
		}
	}
	if end < 0 {
		return 0
	}
	return end
}

func spansFit(spans []redactSpan, rawEnd int) bool {
	for _, sp := range spans {
		if sp.start < rawEnd && sp.end > rawEnd {
			return false
		}
	}
	return true
}

func projectSpans(raw string, spans []redactSpan, rawEnd int) string {
	if rawEnd < 0 {
		rawEnd = 0
	}
	if rawEnd > len(raw) {
		rawEnd = len(raw)
	}
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		if sp.start >= rawEnd {
			break
		}
		if sp.end > rawEnd || sp.start < prev || sp.end < sp.start {
			return ""
		}
		b.WriteString(raw[prev:sp.start])
		b.WriteString(sp.repl)
		prev = sp.end
	}
	b.WriteString(raw[prev:rawEnd])
	return b.String()
}

// learnSpans recovers replacement spans from a pure substring redactor.
// The smallest prefix whose redaction differs is one whole match, found by
// binary search, so a shared look at the redacted text is not required.
func learnSpans(raw, red string, redact func(string) string) ([]redactSpan, bool) {
	if redact == nil {
		return nil, false
	}
	var spans []redactSpan
	i, j := 0, 0
	for i < len(raw) || j < len(red) {
		for i < len(raw) && j < len(red) && raw[i] == red[j] {
			i++
			j++
		}
		if i == len(raw) && j == len(red) {
			return spans, true
		}
		if i >= len(raw) || j > len(red) {
			return nil, false
		}
		d, ok := firstMatchEnd(raw[i:], redact)
		if !ok {
			return nil, false
		}
		repl := redact(raw[i : i+d])
		if len(repl) > len(red)-j || red[j:j+len(repl)] != repl {
			return nil, false
		}
		spans = append(spans, redactSpan{start: i, end: i + d, repl: repl})
		i += d
		j += len(repl)
	}
	return spans, true
}

// firstMatchEnd returns the end of the first replacement in piece. The cut is
// the shortest d where the redactor's output is exactly redact(piece[:d]) plus
// redact(piece[d:]). A shorter secret that is only a prefix of the real match
// fails that check, so the longer value is consumed whole.
func firstMatchEnd(piece string, redact func(string) string) (int, bool) {
	if redact == nil || piece == "" {
		return 0, false
	}
	full := redact(piece)
	if full == piece {
		return 0, false
	}
	for d := 1; d <= len(piece); d++ {
		head := redact(piece[:d])
		if head == piece[:d] {
			continue
		}
		if head+redact(piece[d:]) == full {
			return d, true
		}
	}
	return 0, false
}

// coverNeedles finds the leftmost, then longest, needle at each position.
// needles must be in sortNeedles order and idx built from them.
func coverNeedles(raw string, needles []redactNeedle, idx *needleIndex) ([]redactSpan, bool) {
	if len(needles) == 0 || raw == "" || idx == nil {
		return nil, false
	}
	var spans []redactSpan
	i := 0
	for i < len(raw) {
		hit := -1
		for _, n := range idx[raw[i]] {
			old := needles[n].old
			if i+len(old) > len(raw) || raw[i:i+len(old)] != old {
				continue
			}
			hit = int(n)
			break
		}
		if hit < 0 {
			i++
			continue
		}
		spans = append(spans, redactSpan{
			start: i,
			end:   i + len(needles[hit].old),
			repl:  needles[hit].repl,
		})
		i += len(needles[hit].old)
	}
	if len(spans) == 0 {
		return nil, false
	}
	return spans, true
}

func (w *jobWriter) span() (written, discarded int) {
	if w == nil {
		return 0, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written, w.discarded
}

func (w *jobWriter) held() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending)
}

// snapshot commits completed lines and returns the retained file plus stream offsets.
func (w *jobWriter) snapshot() (body []byte, written, discarded int, err error) {
	if w == nil {
		return nil, 0, 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.flushBlock(false); err != nil {
		return nil, 0, 0, err
	}
	if w.f == nil {
		return nil, w.written, w.discarded, nil
	}
	body, err = io.ReadAll(io.NewSectionReader(w.f, 0, int64(w.fileStable)))
	if err != nil {
		return nil, 0, 0, err
	}
	return body, w.written, w.discarded, nil
}

func (w *jobWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.flushBlock(true)
	err2 := w.f.Close()
	w.f = nil
	if err != nil {
		return err
	}
	return err2
}
