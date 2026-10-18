package kernel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"xbot/internal/tools"
	"xbot/internal/vault"
)

const (
	// maxFreshBytes caps the bytes hashed for a freshness check. Larger files
	// are compared by size plus a prefix hash.
	maxFreshBytes = 4 << 20
	// maxPreviewBytes is how much of a file is kept so a later reminder can
	// show a short diff without rereading the old bytes from disk.
	maxPreviewBytes = 8 << 10
	maxTrackedFiles = 64
	// maxDiffBytes caps one external-change diff. A few kilobytes is enough
	// for the model to see what moved without dragging the file into the prompt.
	maxDiffBytes = 4 << 10
	maxDiffFiles = 8

	fileChangeCooldown = time.Minute

	msgNotRead = "has not been read in this session; read it before writing"
	msgStale   = "changed on disk since it was read at step"
	msgReceipt = "unchanged since last read at step"
	msgRefresh = "record refreshed; no need to reread to verify"
	msgDenied  = "already denied by the owner with these arguments; do not retry this call unchanged"
)

// fileRec is one file this bot has read or written during this kernel's life.
// turn is the turn that recorded it. A short receipt is only valid while that
// same turn is still the one running, so a later turn (and anything compacted
// out of it) reads the bytes again.
type fileRec struct {
	mtime   time.Time
	size    int64
	hash    string
	partial bool
	step    int
	full    bool
	preview string
	label   string
	seq     uint64
	turn    *turn
}

type fileSnap struct {
	mtime   time.Time
	size    int64
	hash    string
	partial bool
	lines   int
	preview string
}

// freshState is the files this kernel has read or written, oldest dropped past
// maxTrackedFiles. It lives for the kernel's lifetime and is not persisted: a
// new process starts empty. The kernel owns one per bot, like cwdState.
type freshState struct {
	mu  sync.Mutex
	rec map[string]*fileRec
	n   uint64
}

type readParams struct {
	Offset int  `json:"offset"`
	Limit  int  `json:"limit"`
	Force  bool `json:"force"`
}

type pathViewer interface {
	ShownPath(abs string) string
}

func modelPath(tool tools.Tool, abs string) string {
	if v, ok := tool.(pathViewer); ok {
		if s := v.ShownPath(abs); s != "" {
			return s
		}
	}
	return abs
}

func toolPath(ctx context.Context, tool tools.Tool, args json.RawMessage) (string, bool) {
	if pt, ok := tool.(tools.PathContext); ok {
		p := pt.ResolvedPathContext(ctx, args)
		if p == "" {
			return "", false
		}
		return p, true
	}
	pt, ok := tool.(tools.PathTool)
	if !ok {
		return "", false
	}
	p := pt.ResolvedPath(args)
	if p == "" {
		return "", false
	}
	return p, true
}

func (rec *fileRec) matches(snap fileSnap) bool {
	return rec != nil && rec.size == snap.size && rec.hash == snap.hash && rec.partial == snap.partial
}

// freshEnough reports whether path still has the bytes rec recorded.
// Equal size and mtime, on a complete hash, skip reading the file again. A
// same-size, same-mtime content swap therefore looks unchanged until some
// later check actually hashes. A newer mtime with the same hash is clock
// jitter and still counts as fresh; the returned snap carries that mtime.
func freshEnough(rec *fileRec, path string) (fileSnap, bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		return fileSnap{}, false, err
	}
	if st.IsDir() {
		return fileSnap{}, false, fmt.Errorf("directory")
	}
	if rec != nil && !rec.partial && st.Size() == rec.size && st.ModTime().Equal(rec.mtime) {
		return fileSnap{mtime: st.ModTime(), size: st.Size(), hash: rec.hash}, true, nil
	}
	snap, err := snapshotFile(path)
	if err != nil {
		return fileSnap{}, false, err
	}
	if rec == nil {
		return snap, false, nil
	}
	return snap, rec.matches(snap), nil
}

// snapshotFile stats and hashes path. A missing file returns the os error.
// The hash is streamed. The read buffer follows the file size up to 32 KiB,
// and only the first maxPreviewBytes are kept for a later diff.
func snapshotFile(path string) (fileSnap, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileSnap{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fileSnap{}, err
	}
	if st.IsDir() {
		return fileSnap{}, fmt.Errorf("directory")
	}
	h := sha256.New()
	buf := make([]byte, snapshotBufSize(st.Size()))
	preview := make([]byte, 0, previewCap(st.Size()))
	var hashed int
	var lines int
	var last byte
	for hashed < maxFreshBytes {
		want := len(buf)
		if remain := maxFreshBytes - hashed; want > remain {
			want = remain
		}
		n, rerr := f.Read(buf[:want])
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return fileSnap{}, werr
			}
			if len(preview) < maxPreviewBytes {
				take := n
				if room := maxPreviewBytes - len(preview); take > room {
					take = room
				}
				preview = append(preview, buf[:take]...)
			}
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
			hashed += n
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fileSnap{}, rerr
		}
		if n == 0 {
			break
		}
	}
	partial := false
	if hashed >= maxFreshBytes {
		var one [1]byte
		n, rerr := f.Read(one[:])
		if n > 0 {
			partial = true
		}
		if rerr != nil && rerr != io.EOF {
			return fileSnap{}, rerr
		}
	}
	if partial {
		lines = 0
	} else if hashed > 0 && last != '\n' {
		lines++
	}
	return fileSnap{
		mtime:   st.ModTime(),
		size:    st.Size(),
		hash:    hex.EncodeToString(h.Sum(nil)),
		partial: partial,
		lines:   lines,
		preview: string(preview),
	}, nil
}

func snapshotBufSize(size int64) int {
	const cap = 32 << 10
	if size <= 0 {
		return 1
	}
	if size < int64(cap) {
		return int(size)
	}
	return cap
}

func previewCap(size int64) int {
	if size <= 0 {
		return 0
	}
	if size > int64(maxPreviewBytes) {
		return maxPreviewBytes
	}
	return int(size)
}

// unchangedReceipt is the short stand-in for a second full read in the same
// turn, when the bytes on disk still match. A later turn reads the body again:
// the earlier tool result may have been compacted, and "step N" would point
// at a counter the model can no longer see.
func (k *Kernel) unchangedReceipt(ctx context.Context, tool tools.Tool, args json.RawMessage) (string, bool) {
	if k == nil || tool == nil || tool.Name() != "read_file" {
		return "", false
	}
	var in readParams
	if json.Unmarshal(args, &in) != nil {
		return "", false
	}
	if in.Force || in.Offset > 1 || in.Limit > 0 {
		return "", false
	}
	path, ok := toolPath(ctx, tool, args)
	if !ok {
		return "", false
	}
	k.files.mu.Lock()
	rec := k.files.rec[path]
	var copy fileRec
	if rec != nil {
		copy = *rec
	}
	k.files.mu.Unlock()
	if rec == nil || !copy.full || copy.turn == nil || copy.turn != k.turnFor(ctx) {
		return "", false
	}
	snap, okMatch, err := freshEnough(&copy, path)
	if err != nil || !okMatch {
		return "", false
	}
	if !snap.mtime.Equal(copy.mtime) {
		k.touchMtime(path, snap.mtime)
	}
	label := copy.label
	if label == "" {
		label = modelPath(tool, path)
	}
	return fmt.Sprintf("%s %d (%s, %d bytes). Pass force or a line range to read it again.", msgReceipt, copy.step, label, copy.size), true
}

// rejectUnreadOrStale blocks edit/write of an existing file the model has not
// read in this session, and of a file whose bytes changed since that read.
// A file that does not exist yet can be created without a prior read.
func (k *Kernel) rejectUnreadOrStale(ctx context.Context, tool tools.Tool, args json.RawMessage) error {
	if k == nil || tool == nil || !isFileEdit(tool.Name()) {
		return nil
	}
	path, ok := toolPath(ctx, tool, args)
	if !ok {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	label := modelPath(tool, path)
	k.files.mu.Lock()
	rec := k.files.rec[path]
	var copy *fileRec
	if rec != nil {
		c := *rec
		copy = &c
	}
	k.files.mu.Unlock()
	name := tool.Name()
	if copy == nil {
		return fmt.Errorf("%s: %s %s", name, label, msgNotRead)
	}
	snap, okMatch, err := freshEnough(copy, path)
	if err != nil {
		return nil
	}
	if !okMatch {
		return fmt.Errorf("%s: %s %s %d; read it again before writing", name, label, msgStale, copy.step)
	}
	if !snap.mtime.Equal(copy.mtime) {
		k.touchMtime(path, snap.mtime)
	}
	return nil
}

// noteRead records a successful read. A ranged or truncated read still stores
// the hash, but a later plain read returns the body until a full view exists.
func (k *Kernel) noteRead(ctx context.Context, tool tools.Tool, args json.RawMessage, out string) {
	if k == nil || tool == nil {
		return
	}
	path, ok := toolPath(ctx, tool, args)
	if !ok {
		return
	}
	snap, err := snapshotFile(path)
	if err != nil {
		return
	}
	var in readParams
	_ = json.Unmarshal(args, &in)
	full := readCovered(out, snap)
	// A range that starts past the first line has not seen the whole file,
	// even when the returned span happens to reach the end.
	if in.Offset > 1 {
		full = false
	}
	k.storeFile(path, snap, k.currentStep(ctx), full, modelPath(tool, path), k.turnFor(ctx))
}

// noteWrite refreshes the record from the bytes just written so the model
// does not have to read the file back to prove the write. It reports whether
// a record was stored. write_file may mark the file fully seen, because the
// model supplied the whole body. edit_file keeps the previous full or partial
// flag: a one-line edit after a ranged read has still not shown the rest.
func (k *Kernel) noteWrite(ctx context.Context, tool tools.Tool, args json.RawMessage) bool {
	if k == nil || tool == nil {
		return false
	}
	path, ok := toolPath(ctx, tool, args)
	if !ok {
		return false
	}
	snap, err := snapshotFile(path)
	if err != nil {
		return false
	}
	full := !snap.partial
	if tool.Name() == "edit_file" {
		full = false
		k.files.mu.Lock()
		if rec := k.files.rec[path]; rec != nil {
			full = rec.full
		}
		k.files.mu.Unlock()
	}
	k.storeFile(path, snap, k.currentStep(ctx), full, modelPath(tool, path), k.turnFor(ctx))
	return true
}

// noteShellWrites refreshes the records of tracked files that a finished
// shell command changed (sed -i, a formatter, a redirect), so the next step
// does not report them as someone else's edit. Only a change whose mtime is
// inside the call's window counts. The model saw the command, not the bytes,
// so the refreshed record is never a full read. It returns the labels.
func (k *Kernel) noteShellWrites(ctx context.Context, start, end time.Time) []string {
	if k == nil {
		return nil
	}
	type item struct {
		path string
		rec  fileRec
	}
	k.fileMu.Lock()
	items := make([]item, 0, len(k.files))
	for p, r := range k.files {
		if r != nil {
			items = append(items, item{p, *r})
		}
	}
	k.fileMu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].rec.seq < items[j].rec.seq })
	var labels []string
	for _, it := range items {
		st, err := os.Stat(it.path)
		if err != nil || st.IsDir() {
			continue
		}
		mt := st.ModTime()
		if mt.Equal(it.rec.mtime) && st.Size() == it.rec.size {
			continue
		}
		if !inCallWindow(mt, start, end) {
			continue
		}
		snap, err := snapshotFile(it.path)
		if err != nil {
			continue
		}
		if it.rec.matches(snap) {
			k.touchMtime(it.path, snap.mtime)
			continue
		}
		label := it.rec.label
		k.storeFile(it.path, snap, k.currentStep(ctx), false, label, k.turnFor(ctx))
		if label == "" {
			label = it.path
		}
		labels = append(labels, label)
	}
	return labels
}

// inCallWindow reports an mtime between start and end. A filesystem that keeps
// whole seconds stores a truncated time, so the window then starts at start's
// second.
func inCallWindow(mt, start, end time.Time) bool {
	from := start
	if mt.Nanosecond() == 0 {
		from = start.Truncate(time.Second)
	}
	return !mt.Before(from) && !mt.After(end)
}

func readCovered(out string, snap fileSnap) bool {
	if strings.Contains(out, "[truncated]") {
		return false
	}
	if strings.Contains(out, "past end of file") {
		return snap.size == 0
	}
	if snap.partial {
		return false
	}
	first, last, ok := parseLineSpan(out)
	if !ok || first > 1 {
		return false
	}
	if snap.lines == 0 {
		return snap.size == 0
	}
	return last >= snap.lines
}

func parseLineSpan(out string) (first, last int, ok bool) {
	line := out
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		line = out[:i]
	}
	i := strings.LastIndex(line, "lines ")
	if i < 0 {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(line[i:], "lines %d-%d", &first, &last); err != nil {
		return 0, 0, false
	}
	return first, last, true
}

func (k *Kernel) currentStep(ctx context.Context) int {
	if k == nil {
		return 0
	}
	t := k.turnFor(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tools
}

func (k *Kernel) storeFile(path string, snap fileSnap, step int, full bool, label string, tr *turn) {
	k.files.mu.Lock()
	defer k.files.mu.Unlock()
	if k.files.rec == nil {
		k.files.rec = map[string]*fileRec{}
	}
	k.files.n++
	k.files.rec[path] = &fileRec{
		mtime:   snap.mtime,
		size:    snap.size,
		hash:    snap.hash,
		partial: snap.partial,
		step:    step,
		full:    full && !snap.partial,
		preview: snap.preview,
		label:   label,
		seq:     k.files.n,
		turn:    tr,
	}
	for len(k.files.rec) > maxTrackedFiles {
		var drop string
		var oldest uint64 = ^uint64(0)
		for p, r := range k.files.rec {
			if r != nil && r.seq < oldest {
				oldest = r.seq
				drop = p
			}
		}
		if drop == "" {
			return
		}
		delete(k.files.rec, drop)
	}
}

func (k *Kernel) touchMtime(path string, m time.Time) {
	k.files.mu.Lock()
	defer k.files.mu.Unlock()
	if r := k.files.rec[path]; r != nil {
		r.mtime = m
	}
}

func appendNote(out, note string) string {
	if note == "" {
		return out
	}
	if strings.TrimSpace(out) == "" {
		return note
	}
	return out + "\n" + note
}

// fileChangeTrigger reports external edits to files this bot has read.
// The channel supplies the cooldown. The same path and content hash is not
// described twice.
func (k *Kernel) fileChangeTrigger() func(time.Time, []Reminder) string {
	return func(_ time.Time, prev []Reminder) string {
		body := k.fileChangeNote(prev)
		if body == "" {
			return ""
		}
		return "<file_change>\nThese files changed on disk after they were read. Read them again before editing.\n" + body + "\n</file_change>"
	}
}

func (k *Kernel) fileChangeNote(prev []Reminder) string {
	if k == nil {
		return ""
	}
	type item struct {
		path string
		rec  fileRec
	}
	k.files.mu.Lock()
	items := make([]item, 0, len(k.files.rec))
	for p, r := range k.files.rec {
		if r != nil {
			items = append(items, item{p, *r})
		}
	}
	k.files.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].rec.seq < items[j].rec.seq })
	var b strings.Builder
	n := 0
	for _, it := range items {
		if n >= maxDiffFiles || b.Len() >= maxDiffBytes {
			if b.Len() >= maxDiffBytes {
				b.WriteString("[diff truncated]\n")
			}
			break
		}
		label := it.rec.label
		if label == "" {
			label = it.path
		}
		snap, okMatch, err := freshEnough(&it.rec, it.path)
		if err != nil {
			if !os.IsNotExist(err) {
				continue
			}
			if reportedChange(prev, label, "deleted") {
				continue
			}
			fmt.Fprintf(&b, "- %s was deleted since it was read at step %d sig=%s\n", label, it.rec.step, "deleted")
			n++
			continue
		}
		if okMatch {
			if !snap.mtime.Equal(it.rec.mtime) {
				k.touchMtime(it.path, snap.mtime)
			}
			continue
		}
		sig := snap.hash
		if len(sig) > 12 {
			sig = sig[:12]
		}
		if reportedChange(prev, label, sig) {
			continue
		}
		fmt.Fprintf(&b, "- %s changed since it was read at step %d sig=%s\n", label, it.rec.step, sig)
		// Secret-bearing names get the fact of the change and nothing else.
		// A diff or a size line can echo a token that was just written.
		if !sensitivePath(it.path, label) {
			diff := briefDiff(it.rec.preview, snap.preview)
			if diff == "" {
				fmt.Fprintf(&b, "  size %d -> %d\n", it.rec.size, snap.size)
			} else if vault.LooksLikeSecret(diff) {
				// Vault redaction only knows vault values. A token someone else
				// wrote into an ordinary file would ride the diff into context.
				b.WriteString("  diff not shown: it may contain a secret\n")
			} else {
				for _, line := range strings.Split(diff, "\n") {
					b.WriteString("  ")
					b.WriteString(line)
					b.WriteByte('\n')
				}
			}
		}
		n++
	}
	return strings.TrimRight(b.String(), "\n")
}

// sensitivePath reports a file whose contents must not appear in a reminder.
// The check looks at the real path and the path shown to the model.
func sensitivePath(path, label string) bool {
	return sensitiveName(path) || sensitiveName(label)
}

func sensitiveName(p string) bool {
	if p == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(p))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".key", ".pem":
		return true
	}
	switch base {
	case "credentials", "credential", ".netrc":
		return true
	}
	if strings.Contains(base, "credential") {
		return true
	}
	for _, name := range []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"} {
		if base == name || strings.HasPrefix(base, name+".") {
			return true
		}
	}
	return false
}

func reportedChange(prev []Reminder, label, sig string) bool {
	needle := "sig=" + sig
	for _, p := range prev {
		if strings.Contains(p.Text, label) && strings.Contains(p.Text, needle) {
			return true
		}
	}
	return false
}

func briefDiff(old, neu string) string {
	old = strings.TrimRight(strings.ReplaceAll(old, "\r\n", "\n"), "\n")
	neu = strings.TrimRight(strings.ReplaceAll(neu, "\r\n", "\n"), "\n")
	if old == neu {
		return ""
	}
	ob := strings.Split(old, "\n")
	nb := strings.Split(neu, "\n")
	var b strings.Builder
	b.WriteString("--- previous\n+++ current\n@@\n")
	for i := 0; i < len(ob) || i < len(nb); i++ {
		var a, c string
		hasA := i < len(ob)
		hasC := i < len(nb)
		if hasA {
			a = ob[i]
		}
		if hasC {
			c = nb[i]
		}
		if hasA && hasC && a == c {
			continue
		}
		if b.Len() >= maxDiffBytes {
			b.WriteString("[diff truncated]\n")
			break
		}
		if hasA {
			fmt.Fprintf(&b, "- %s\n", oneDiffLine(a))
		}
		if hasC && b.Len() < maxDiffBytes {
			fmt.Fprintf(&b, "+ %s\n", oneDiffLine(c))
		}
	}
	if b.Len() > maxDiffBytes {
		cut := trimUTF8(b.String(), maxDiffBytes)
		return strings.TrimRight(cut, "\n") + "\n[diff truncated]"
	}
	return strings.TrimRight(b.String(), "\n")
}

func oneDiffLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= 80 {
		return s
	}
	budget := 80
	for budget > 0 && !utf8.RuneStart(s[budget]) {
		budget--
	}
	return s[:budget]
}

func canonArgs(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func (k *Kernel) noteDenial(name string, args json.RawMessage) {
	if k == nil || name == "" {
		return
	}
	key := name + "\n" + canonArgs(args)
	k.denyMu.Lock()
	defer k.denyMu.Unlock()
	if k.denied == nil {
		k.denied = map[string]struct{}{}
	}
	k.denied[key] = struct{}{}
}

// clearDenials drops refusals from earlier turns. The next owner message can
// ask again; identical retries inside the turn that was refused still fail.
func (k *Kernel) clearDenials() {
	if k == nil {
		return
	}
	k.denyMu.Lock()
	k.denied = nil
	k.denyMu.Unlock()
}

func (k *Kernel) deniedBefore(name string, args json.RawMessage) bool {
	if k == nil || name == "" {
		return false
	}
	key := name + "\n" + canonArgs(args)
	k.denyMu.Lock()
	defer k.denyMu.Unlock()
	_, ok := k.denied[key]
	return ok
}
