package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// environment is a fold of the effective conversation, never a saved ledger.
// Fields hold absolute published values; memory is folded from the fixed prefix.
// A snapshot resets the fold. Its memory operations are relative to that prefix,
// so an unchanged Bot memory is never repeated in a snapshot.
type environment struct {
	Fields map[string]json.RawMessage
	Memory []string
	Basis  string
}
type environmentSource struct {
	Name   string `json:"name"`
	Sender string `json:"sender,omitempty"`
	Input  int    `json:"input,omitempty"`
}

type memoryChange struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}
type environmentMessage struct {
	Snapshot string                     `json:"snapshot,omitempty"`
	Set      map[string]json.RawMessage `json:"set,omitempty"`
	Memory   *memoryChange              `json:"memory,omitempty"`
}

const environmentOpen = bot.EnvironmentOpen
const environmentClose = bot.EnvironmentClose

func encodeEnvironment(p environmentMessage) string {
	if p.Snapshot == "" && len(p.Set) == 0 && p.Memory == nil {
		return ""
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	// Memory text is already fenced. Escaping its ampersands a second time
	// can push an otherwise bounded profile beyond the tool-result limit.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(p); err != nil {
		panic(err)
	}
	return environmentOpen + strings.TrimSuffix(body.String(), "\n") + environmentClose
}
func decodeEnvironment(text string) (environmentMessage, bool) {
	var p environmentMessage
	if !strings.HasPrefix(text, environmentOpen) || !strings.HasSuffix(text, environmentClose) {
		return p, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, environmentOpen), environmentClose)
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil {
		return environmentMessage{}, false
	}
	// Reject trailing objects and prose, including a quoted envelope.
	if strings.TrimSpace(body) == "" || !json.Valid([]byte(body)) {
		return environmentMessage{}, false
	}
	return p, true
}
func environmentSet(e *environment, key string, value any) {
	if e.Fields == nil {
		e.Fields = map[string]json.RawMessage{}
	}
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	e.Fields[key] = b
}
func environmentGet[T any](e environment, key string) (v T) {
	_ = json.Unmarshal(e.Fields[key], &v)
	return
}
func memoryFacts(text string) []string {
	main, log := splitRecentLog(text)
	out := factLines(main)
	for _, line := range nonBlank(log) {
		out = append(out, "[Recent log] "+line)
	}
	seen := map[string]bool{}
	var unique []string
	for _, line := range out {
		if !seen[line] {
			unique = append(unique, line)
			seen[line] = true
		}
	}
	return unique
}
func exactMemoryChange(old, next []string) *memoryChange {
	add, remove := multisetDiff(old, next)
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	return &memoryChange{Add: add, Remove: remove}
}

// Old recent-log entries leaving the prefix window are not external deletions.
func changedMemory(old, next []string) *memoryChange {
	p := exactMemoryChange(old, next)
	if p == nil {
		return nil
	}
	kept := p.Remove[:0]
	for _, line := range p.Remove {
		if !strings.HasPrefix(line, "[Recent log] ") {
			kept = append(kept, line)
		}
	}
	p.Remove = kept
	if len(p.Add) == 0 && len(p.Remove) == 0 {
		return nil
	}
	return p
}
func applyEnvironment(e *environment, p environmentMessage, prefix []string) {
	if p.Snapshot != "" {
		people := map[string]json.RawMessage{}
		for key, value := range e.Fields {
			if strings.HasPrefix(key, "person:") {
				people[key] = value
			}
		}
		*e = environment{Fields: people, Memory: append([]string(nil), prefix...), Basis: p.Snapshot}
	}
	for key, value := range p.Set {
		if string(value) == "null" {
			delete(e.Fields, key)
		} else {
			e.Fields[key] = value
		}
	}
	if p.Memory != nil {
		for _, line := range p.Memory.Remove {
			e.Memory = removeOnce(e.Memory, line)
		}
		for _, line := range p.Memory.Add {
			if !slices.Contains(e.Memory, line) {
				e.Memory = append(e.Memory, line)
			}
		}
	}
}

// Only kernel-origin user records and receipts from the matching built-in file
// tool can publish state. Summaries, quotes, webpages and owner prose cannot.
func replayEnvironment(msgs []provider.Message, prefix string) environment {
	seed := memoryFacts(prefix)
	e := environment{Fields: map[string]json.RawMessage{}, Memory: append([]string(nil), seed...)}
	calls := map[string]string{}
	for _, m := range msgs {
		for _, tc := range m.Calls() {
			calls[tc.ID] = tc.Function.Name
		}
		text := m.Text()
		if m.Role == provider.RoleTool {
			name := calls[m.CallID()]
			if name != "write_file" && name != "edit_file" {
				continue
			}
			_, receipt, _ := bot.SplitEnvironmentReceipt(text)
			if receipt == "" {
				continue
			}
			text = receipt
		} else if m.Role != provider.RoleUser || !m.Internal {
			continue
		}
		if p, ok := decodeEnvironment(text); ok {
			applyEnvironment(&e, p, seed)
		}
	}
	return e
}
func environmentDelta(prev, next environment, snapshot, prefix string) string {
	p := environmentMessage{Snapshot: snapshot, Set: map[string]json.RawMessage{}}
	before := prev.Fields
	mem := prev.Memory
	if snapshot != "" {
		before = nil
		mem = memoryFacts(prefix)
	}
	for key, value := range next.Fields {
		if !reflect.DeepEqual(before[key], value) {
			p.Set[key] = value
		}
	}
	for key := range before {
		if _, ok := next.Fields[key]; !ok {
			p.Set[key] = json.RawMessage("null")
		}
	}
	p.Memory = changedMemory(mem, next.Memory)
	if snapshot != "" {
		p.Memory = exactMemoryChange(mem, next.Memory)
	}
	return encodeEnvironment(p)
}
func environmentBasis(st bot.State, msgs []provider.Message) string {
	var parts []string
	for _, b := range st.Blocks {
		parts = append(parts, fmt.Sprintf("%d:%d", b.From, b.To))
	}
	for _, m := range msgs {
		if m.Internal && strings.HasPrefix(m.Text(), "<turn_progress") {
			first, _, _ := strings.Cut(m.Text(), "\n")
			parts = append(parts, first)
		}
	}
	return shortHash(st.MemSnap + "\n" + strings.Join(parts, "\n"))
}
func (k *Kernel) visibleEnvironment(st bot.State) (environment, error) {
	lines, err := k.Bot.UncoveredLines(st.Blocks)
	if err != nil {
		return environment{}, err
	}
	var entries []bot.Entry
	var seqs []int
	for _, line := range lines {
		entries = append(entries, line.Entry)
		seqs = append(seqs, line.Seq)
	}
	return replayEnvironment(projected(st.Blocks, entries, seqs), st.MemSnap), nil
}
func environmentTasks(e environment) []bot.TaskEnv {
	var out []bot.TaskEnv
	for key := range e.Fields {
		if strings.HasPrefix(key, "task:") {
			out = append(out, environmentGet[bot.TaskEnv](e, key))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (k *Kernel) appendEnvironment(text string) (provider.Message, error) {
	if text == "" {
		return provider.Message{}, nil
	}
	m := provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(text)}
	seq, err := k.Bot.AppendSeq(bot.Entry{Time: k.now(), Origin: bot.OriginInternal, Message: provider.Message{Role: m.Role, Content: provider.TextContent(m.Text())}})
	m.Seq = seq
	return m, err
}

// sampleEnvironment reuses live configuration, task owners and memory storage.
// The incoming event's identity is already in the conversation; it is not
// re-applied from an older turn when a newer person's input has arrived.
func (k *Kernel) sampleEnvironment(prev environment) (environment, error) {
	next := environment{Fields: map[string]json.RawMessage{}, Memory: append([]string(nil), prev.Memory...)}
	for key, value := range prev.Fields {
		next.Fields[key] = value
	}
	for _, key := range []string{"persona", "rules", "skills", "profile", "overlay"} {
		var v string
		var err error
		switch key {
		case "persona":
			v, err = k.Bot.PersonaText()
		case "rules":
			v, err = k.rulesText()
		case "skills":
			var lines []string
			lines, err = k.skillLines()
			v = strings.Join(lines, "\n")
		case "profile":
			v = k.Bot.ProfileText()
		case "overlay":
			var key, text string
			key, text, err = k.modelOverlay()
			v = "none"
			if text != "" {
				v = key + " " + shortHash(text)
			}
		}
		if err != nil {
			return next, err
		}
		if key != "overlay" {
			v = shortHash(v)
		}
		environmentSet(&next, key, v)
	}
	list, err := k.modelTools()
	if err != nil {
		return next, err
	}
	names := toolNames(list)
	if provider.HasHostedSearch(k.boundProvider()) {
		names = append(names, "hosted:web_search")
	}
	environmentSet(&next, "tools", names)
	environmentSet(&next, "notes", k.pendingNotes())
	environmentSet(&next, "time", k.localStamp())
	tasks, err := k.taskEnvironment(environmentTasks(prev), link.Event{})
	if err != nil {
		return next, err
	}
	for key := range next.Fields {
		if strings.HasPrefix(key, "task:") {
			delete(next.Fields, key)
		}
	}
	for _, task := range tasks {
		environmentSet(&next, "task:"+task.ID, task)
	}
	ctx := context.WithValue(context.Background(), publishedMemoryRead{}, true)
	if id := environmentGet[environmentSource](next, "source").Sender; id != "" {
		text, err := k.personText(ctx, link.Event{Sender: id, SenderKind: link.KindPerson})
		if err != nil && !errors.Is(err, memory.ErrBusy) {
			return next, err
		}
		if err == nil {
			environmentSet(&next, "person:"+id, text)
		}
	}
	text, err := k.memoryText(ctx, "")
	if errors.Is(err, memory.ErrBusy) {
		return next, nil
	}
	if err != nil {
		return next, err
	}
	next.Memory = memoryFacts(text)
	return next, nil
}
func (k *Kernel) publishEnvironment(st bot.State, prev environment, full bool, basis string) (provider.Message, error) {
	next, err := k.sampleEnvironment(prev)
	if err != nil {
		return provider.Message{}, err
	}
	if full {
		// Only the current person's full profile is restored. Other people establish
		// their baseline on their next appearance after their records are covered.
		current := environmentGet[environmentSource](next, "source").Sender
		for key := range next.Fields {
			if strings.HasPrefix(key, "person:") && (current == "" || key != "person:"+current) {
				delete(next.Fields, key)
			}
		}
	} else {
		basis = ""
	}
	return k.appendEnvironment(environmentDelta(prev, next, basis, st.MemSnap))
}
func (k *Kernel) receiveEnvironment(ev link.Event, seq int) error {
	st, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	prev, err := k.visibleEnvironment(st)
	if err != nil {
		return err
	}
	next := environment{Fields: map[string]json.RawMessage{}, Memory: prev.Memory}
	for key, value := range prev.Fields {
		next.Fields[key] = value
	}
	previous := environmentGet[environmentSource](prev, "source")
	if seq < previous.Input {
		return nil
	}
	source := environmentSource{Name: ev.Source, Input: seq}
	if personEvent(ev) {
		source.Sender = ev.Sender
	}
	if source.Name == previous.Name && source.Sender == previous.Sender {
		source.Input = previous.Input
	}
	environmentSet(&next, "source", source)
	if personEvent(ev) {
		ctx := context.WithValue(context.Background(), publishedMemoryRead{}, true)
		text, err := k.personText(ctx, ev)
		if err != nil && !errors.Is(err, memory.ErrBusy) {
			return err
		}
		if err == nil {
			environmentSet(&next, "person:"+ev.Sender, text)
		}
	}
	_, err = k.appendEnvironment(environmentDelta(prev, next, "", st.MemSnap))
	return err
}
func (k *Kernel) syncEnv(st bot.State, _ link.Event, _ string) (bot.State, error) {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	prev, err := k.visibleEnvironment(st)
	if err != nil {
		return st, err
	}
	_, err = k.publishEnvironment(st, prev, prev.Basis != environmentBasis(st, nil), environmentBasis(st, nil))
	if err != nil {
		return st, err
	}
	return st, k.Bot.SaveState(st)
}
func (k *Kernel) ensureSnapshot(st bot.State) error {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	prev, err := k.visibleEnvironment(st)
	if err != nil {
		return err
	}
	if prev.Basis == environmentBasis(st, nil) {
		return nil
	}
	_, err = k.publishEnvironment(st, prev, true, environmentBasis(st, nil))
	return err
}

// The receipt is part of the original tool result, not an out-of-band own-write
// queue. The folder guard has already committed (or restored) the actual file.
func (k *Kernel) beginOwnWrite(ctx context.Context, scope memory.Scope, personID ...string) func(bool) string {
	k.memWriteMu.Lock()
	key := ""
	if scope == memory.ScopePerson && len(personID) > 0 {
		key = personID[0]
	}
	read := func() (string, error) {
		if key != "" {
			return k.personText(ctx, link.Event{Sender: key, SenderKind: link.KindPerson})
		}
		return k.memoryText(ctx, "")
	}
	before, readErr := read()
	return func(ok bool) string {
		defer k.memWriteMu.Unlock()
		if !ok || readErr != nil {
			return ""
		}
		after, err := read()
		if err != nil || before == after {
			return ""
		}
		p := environmentMessage{}
		if key != "" {
			p.Set = map[string]json.RawMessage{}
			b, _ := json.Marshal(after)
			p.Set["person:"+key] = b
		} else {
			p.Memory = changedMemory(memoryFacts(before), memoryFacts(after))
		}
		text := encodeEnvironment(p)
		if text != "" {
			return "\n\n" + text
		}
		return ""
	}
}

// finishEnvironment sees the actual fitted view. A surviving unrelated update
// cannot stand in for the full snapshot of this compression boundary.
func (r *turnRun) finishEnvironment(ctx context.Context, raw, fitted provider.Request) (provider.Request, error) {
	if r.k.AllowFork {
		return fitted, nil
	}
	k := r.k
	k.envMu.Lock()
	defer k.envMu.Unlock()
	if err := ctx.Err(); err != nil {
		return fitted, err
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return fitted, err
	}
	before := replayEnvironment(fitted.Messages, st.MemSnap)
	basis := environmentBasis(st, fitted.Messages)
	full := before.Basis != basis
	source := before
	if full {
		// The unfitted view is used only to identify the real source and live task
		// owners. Published baselines still come exclusively from the fitted view.
		source = replayEnvironment(raw.Messages, st.MemSnap)
	}
	// A new input may arrive after prepare and before this lock. Calibrate
	// only the real source from the latest persisted conversation; do not use
	// that wider view as the fitted request's published baseline.
	latest, err := k.visibleEnvironment(st)
	if err != nil {
		return fitted, err
	}
	incoming := environmentGet[environmentSource](latest, "source")
	if incoming.Input > environmentGet[environmentSource](source, "source").Input {
		fields := map[string]json.RawMessage{}
		for key, value := range source.Fields {
			fields[key] = value
		}
		source.Fields = fields
		environmentSet(&source, "source", incoming)
	}
	next, err := k.sampleEnvironment(source)
	if err != nil {
		return fitted, err
	}
	if full {
		current := environmentGet[environmentSource](next, "source").Sender
		for key := range next.Fields {
			if strings.HasPrefix(key, "person:") && key != "person:"+current {
				delete(next.Fields, key)
			}
		}
	} else {
		basis = ""
	}
	m, err := k.appendEnvironment(environmentDelta(before, next, basis, st.MemSnap))
	if err != nil {
		return fitted, err
	}
	if m.Text() != "" {
		fitted.Messages = append(fitted.Messages, m)
		// The full replacement was reserved by fit before any cut. Like the
		// existing fitter, keep the latest tool step intact even when that
		// step alone is larger than H; do not invent a second truncation rule.

	}
	return fitted, nil
}

// No second memory snapshot is stored: a busy scope keeps the last valid
// publication in the effective conversation until its actual commit finishes.
type publishedMemoryRead struct{}

func (k *Kernel) recallMemory(ctx context.Context, q memory.Query) ([]memory.Fact, error) {
	if ctx.Value(publishedMemoryRead{}) != nil {
		if store, ok := k.Memory.(interface {
			TryRecall(context.Context, memory.Query) ([]memory.Fact, error)
		}); ok {
			return store.TryRecall(ctx, q)
		}
		if !k.memWriteMu.TryLock() {
			return nil, memory.ErrBusy
		}
		defer k.memWriteMu.Unlock()
	}
	return k.Memory.Recall(ctx, q)
}
