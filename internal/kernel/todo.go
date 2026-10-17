package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/review"
)

const (
	// defaultTodoStaleSteps is how many tool steps a list must sit unchanged
	// before a reminder. A step is one model round that called tools.
	defaultTodoStaleSteps = 6
	// todoCooldown is the minimum gap between todo reminders, including the
	// completion gate, so a fast loop cannot nag on every step.
	todoCooldown = 2 * time.Minute

	todoMaxItems = 20
	todoMaxRunes = 200
	todoFileMode = 0600
	todoSummaryN = 80
)

var todoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// TodoItem is one line of a bot's task list.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

// todoFile is the on-disk list for one bot. Step counts tool rounds since
// the file was created. UpdatedStep is the round of the last successful
// todo write. Reminded is the list last sent as a reminder, so the next
// note is only the difference.
type todoFile struct {
	Items       []TodoItem `json:"items,omitempty"`
	Step        int        `json:"step,omitempty"`
	UpdatedStep int        `json:"updated_step,omitempty"`
	Reminded    []TodoItem `json:"reminded,omitempty"`
}

func (k *Kernel) todoPath() string {
	if k == nil || k.Bot == nil {
		return ""
	}
	return TodoPath(k.Bot)
}

// TodoPath is bots/<name>/todos.json. The active list is the current task.
func TodoPath(b *bot.Bot) string {
	if b == nil {
		return ""
	}
	return filepath.Join(bot.Dir(b.Root, b.Name()), "todos.json")
}

// TodoArchivePath is bots/<name>/todos-archive.jsonl. Finished lists and
// lists replaced by a new task are appended here, one object per line.
func TodoArchivePath(b *bot.Bot) string {
	if b == nil {
		return ""
	}
	return filepath.Join(bot.Dir(b.Root, b.Name()), "todos-archive.jsonl")
}

// ReadTodos returns the stored list. A missing file is an empty list.
func ReadTodos(b *bot.Bot) ([]TodoItem, error) {
	f, err := readTodoFile(TodoPath(b))
	if err != nil {
		return nil, err
	}
	if f.Items == nil {
		return []TodoItem{}, nil
	}
	return f.Items, nil
}

func readTodoFile(path string) (todoFile, error) {
	if path == "" {
		return todoFile{}, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return todoFile{}, nil
	}
	if err != nil {
		return todoFile{}, err
	}
	if len(raw) == 0 {
		return todoFile{}, nil
	}
	var f todoFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return todoFile{}, err
	}
	return f, nil
}

func writeTodoFile(path string, f todoFile) error {
	if path == "" {
		return errors.New("todo: no bot")
	}
	raw, err := fsutil.Marshal(f)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, todoFileMode)
}

// todoPolicy says whether reminders and the tool are on, and how many quiet
// steps must pass. Kernel.TodoSteps wins when set. Otherwise the model spec's
// todo_steps wins, then 6. An explicit model spec todo flag wins over the
// deepseek default. An empty binding is the product default, deepseek-chat.
func (k *Kernel) todoPolicy() (enabled bool, steps int) {
	steps = defaultTodoStaleSteps
	if k == nil {
		return false, steps
	}
	k.todoMu.Lock()
	defer k.todoMu.Unlock()
	if k.todoCached {
		return k.todoOnV, k.todoStepsV
	}
	if k.TodoSteps > 0 {
		steps = k.TodoSteps
	}
	if k.Bot != nil {
		cfg := k.Bot.Config()
		prov := strings.TrimSpace(cfg.Provider)
		model := strings.TrimSpace(cfg.Model)
		if k.Bot.Root != "" && prov != "" {
			if _, spec, err := config.Lookup(k.Bot.Root, prov, model); err == nil {
				if k.TodoSteps == 0 && spec.TodoSteps > 0 {
					steps = spec.TodoSteps
				}
				if spec.Todo != nil {
					enabled = *spec.Todo
					k.todoOnV = enabled
					k.todoStepsV = steps
					k.todoCached = true
					return enabled, steps
				}
			}
		}
		enabled = deepseekDefault(prov, model)
	}
	k.todoOnV = enabled
	k.todoStepsV = steps
	k.todoCached = true
	return enabled, steps
}

func deepseekDefault(provider, model string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.ToLower(strings.TrimSpace(model))
	if p == "" && m == "" {
		return true
	}
	return strings.Contains(p, "deepseek") || strings.Contains(m, "deepseek")
}

// todoQuiet is true when every tool besides the links catalog and todo is
// read-only. Those runs get no todo reminder and no completion gate. The
// answer is cached on the turn so a step does not rebuild the tool list.
func (k *Kernel) todoQuiet() bool {
	if k == nil {
		return true
	}
	t := k.liveTurn()
	if t == nil {
		return k.todoToolsQuiet()
	}
	t.mu.Lock()
	if t.todoQuietSet {
		v := t.todoQuietV
		t.mu.Unlock()
		return v
	}
	t.mu.Unlock()
	v := k.todoToolsQuiet()
	t.mu.Lock()
	t.todoQuietV = v
	t.todoQuietSet = true
	t.mu.Unlock()
	return v
}

func (k *Kernel) todoToolsQuiet() bool {
	list, err := k.modelTools()
	if err != nil {
		return false
	}
	saw := false
	for _, t := range list {
		if t == nil {
			continue
		}
		name := t.Name()
		if name == "" || name == "todo" || name == "links" {
			continue
		}
		saw = true
		if review.ClassOf(name) != review.Readonly {
			return false
		}
	}
	return saw
}

func (k *Kernel) todoToolOn() bool {
	if k == nil {
		return false
	}
	on, _ := k.todoPolicy()
	return on
}

func (k *Kernel) turnIsTimer() bool {
	t := k.liveTurn()
	return t != nil && t.turnSource == link.NameTimer
}

func (k *Kernel) loadTodo() (todoFile, error) {
	return readTodoFile(k.todoPath())
}

func (k *Kernel) saveTodo(f todoFile) error {
	return writeTodoFile(k.todoPath(), f)
}

// todoStateLocked returns the in-memory list, loading it once. The caller
// holds todoMu. Step changes stay here until a write or a reminder saves.
func (k *Kernel) todoStateLocked() (todoFile, error) {
	if k.todoLoaded {
		return k.todoMem, nil
	}
	f, err := k.loadTodo()
	if err != nil {
		return todoFile{}, err
	}
	k.todoMem = f
	k.todoLoaded = true
	return f, nil
}

// noteTodoStep counts one tool round in memory. The switch being off, a
// timer wake, or a read-only run does not touch the file. A successful todo
// write in this round marks the list fresh, so the reminder cannot fire on
// the same step.
func (k *Kernel) noteTodoStep(msgs []provider.Message) {
	if k == nil || k.turnIsTimer() {
		return
	}
	on, _ := k.todoPolicy()
	if !on || k.todoQuiet() {
		return
	}
	updated := stepUpdatedTodo(msgs)
	k.todoMu.Lock()
	defer k.todoMu.Unlock()
	f, err := k.todoStateLocked()
	if err != nil {
		return
	}
	if len(f.Items) == 0 && !updated {
		return
	}
	f.Step++
	if updated {
		f.UpdatedStep = f.Step
	}
	k.todoMem = f
}

func stepUpdatedTodo(msgs []provider.Message) bool {
	for _, m := range msgs {
		if m.Role != provider.RoleTool || m.Name != "todo" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(m.Content), "error:") {
			continue
		}
		return true
	}
	return false
}

func (k *Kernel) todoTrigger() func(now time.Time, prev []Reminder) string {
	return func(now time.Time, prev []Reminder) string {
		return k.todoNote(now, prev)
	}
}

// todoNote is the stale-list reminder. It returns "" unless the active list
// exists, the model switch is on, the run is not a timer wake or read-only,
// at least N tool steps have passed since the last update, the cooldown has
// elapsed, and something changed since the previous reminder.
func (k *Kernel) todoNote(now time.Time, prev []Reminder) string {
	if k == nil || k.turnIsTimer() {
		return ""
	}
	on, steps := k.todoPolicy()
	if !on || k.todoQuiet() {
		return ""
	}
	if !todoCooled(prev, now) {
		return ""
	}
	k.todoMu.Lock()
	defer k.todoMu.Unlock()
	f, err := k.todoStateLocked()
	if err != nil || len(f.Items) == 0 {
		return ""
	}
	since := f.Step - f.UpdatedStep
	if since <= 0 || since < steps {
		return ""
	}
	text := todoDiffText(f.Items, f.Reminded)
	if text == "" {
		return ""
	}
	f.Reminded = append([]TodoItem(nil), f.Items...)
	k.todoMem = f
	if err := k.saveTodo(f); err != nil {
		return ""
	}
	return text
}

func todoCooled(prev []Reminder, now time.Time) bool {
	var last time.Time
	for _, p := range prev {
		if p.Kind != KindTodo {
			continue
		}
		if p.At.After(last) {
			last = p.At
		}
	}
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= todoCooldown
}

func todoDiffText(cur, prev []TodoItem) string {
	if len(cur) == 0 {
		return ""
	}
	var lines []string
	if len(prev) == 0 {
		for _, it := range cur {
			if todoOpen(it.Status) {
				lines = append(lines, fmt.Sprintf("- [%s] %s", it.Status, it.Content))
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return "<todo_reminder>\nTask list:\n" + strings.Join(lines, "\n") + "\n</todo_reminder>"
	}
	prevBy := make(map[string]TodoItem, len(prev))
	for _, it := range prev {
		prevBy[it.ID] = it
	}
	curBy := make(map[string]TodoItem, len(cur))
	for _, it := range cur {
		curBy[it.ID] = it
		old, ok := prevBy[it.ID]
		if !ok {
			lines = append(lines, fmt.Sprintf("- added [%s] %s", it.Status, it.Content))
			continue
		}
		if old.Status != it.Status || old.Content != it.Content || old.Reason != it.Reason {
			lines = append(lines, fmt.Sprintf("- %s: %s -> %s", it.Content, old.Status, it.Status))
		}
	}
	for _, it := range prev {
		if _, ok := curBy[it.ID]; !ok {
			lines = append(lines, "- removed "+it.Content)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "<todo_reminder>\nTask list changes since the last reminder:\n" + strings.Join(lines, "\n") + "\n</todo_reminder>"
}

func todoOpen(status string) bool {
	return status == "pending" || status == "in_progress"
}

// todoGateText asks the model to finish or explicitly drop unfinished items.
// The caller injects it once per turn.
func todoGateText(items []TodoItem) string {
	var lines []string
	for _, it := range items {
		if !todoOpen(it.Status) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", it.Status, it.Content))
	}
	if len(lines) == 0 {
		return ""
	}
	return "<todo_gate>\nThe task list still has unfinished items. Finish them, or mark the ones you are dropping as dropped and give a reason. Do not stop while they are still open.\n" + strings.Join(lines, "\n") + "\n</todo_gate>"
}

// todoGate decides the one completion-gate continuation for this turn.
// It runs only when this turn wrote the list. A timer wake, a question to
// the owner, or an open approval card does not continue.
func (k *Kernel) todoGate(t *turn, msg provider.Message) (string, bool) {
	if k == nil || t == nil || k.todoQuiet() || k.turnIsTimer() {
		return "", false
	}
	on, _ := k.todoPolicy()
	if !on {
		return "", false
	}
	t.mu.Lock()
	touched := t.todoTouched
	gated := t.todoGated
	asked := t.askedOwner
	t.mu.Unlock()
	if !touched || gated || asked || asksOwner(msg) {
		return "", false
	}
	if len(k.openApprovals()) > 0 {
		return "", false
	}
	k.todoMu.Lock()
	f, err := k.todoStateLocked()
	k.todoMu.Unlock()
	if err != nil {
		return "", false
	}
	text := todoGateText(f.Items)
	if text == "" {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.todoGated {
		return "", false
	}
	t.todoGated = true
	return text, true
}

func asksOwner(msg provider.Message) bool {
	s := strings.TrimSpace(msg.Content)
	return strings.Contains(s, "?") || strings.Contains(s, "？")
}

// TodoSummary is one short line for the status API. Empty when there is no list.
func TodoSummary(b *bot.Bot) string {
	items, err := ReadTodos(b)
	if err != nil || len(items) == 0 {
		return ""
	}
	var pending, progress int
	var current string
	for _, it := range items {
		switch it.Status {
		case "in_progress":
			progress++
			if current == "" {
				current = it.Content
			}
		case "pending":
			pending++
		}
	}
	if progress == 0 && pending == 0 {
		return ""
	}
	var s string
	if current != "" {
		s = "in progress: " + current
		if pending > 0 {
			s += fmt.Sprintf(" · %d pending", pending)
		}
	} else {
		s = fmt.Sprintf("%d pending", pending)
	}
	s = bot.RedactText(s)
	r := []rune(s)
	if len(r) > todoSummaryN {
		s = string(r[:todoSummaryN-1]) + "…"
	}
	return s
}

type todoTool struct{ k *Kernel }

func (todoTool) Name() string { return "todo" }
func (todoTool) Description() string {
	return "Write the task list for a multi-step task of 3 or more steps, then update it as you go. Each item has content and a status of pending, in_progress, or done. At most one item is in_progress. Mark an item dropped, with a reason, when you will not do it. Pass the full list; it replaces the current one. Do not use this for a simple question or a one-step answer."
}
func (todoTool) Parameters() any {
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      stringProp("optional stable id, letters digits _ -"),
			"content": stringProp("what to do, one short line"),
			"status":  map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done", "dropped"}, "description": "pending, in_progress, done, or dropped"},
			"reason":  stringProp("required when status is dropped"),
		},
		"required":             []string{"content", "status"},
		"additionalProperties": false,
	}
	return schema(map[string]any{
		"items": map[string]any{
			"type":        "array",
			"description": "the full task list",
			"items":       item,
		},
	}, []string{"items"})
}
func (todoTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }

func (t todoTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if t.k == nil {
		return "", errors.New("todo: no kernel")
	}
	items, err := parseTodoItems(args, t.k.redactor())
	if err != nil {
		return "", err
	}
	t.k.todoMu.Lock()
	defer t.k.todoMu.Unlock()
	f, err := t.k.todoStateLocked()
	if err != nil {
		return "", err
	}
	if len(f.Items) > 0 && !sameTodoTask(f.Items, items) {
		if err := t.k.appendTodoArchive(f.Items); err != nil {
			return "", err
		}
		f.Reminded = nil
	}
	if !todoHasOpen(items) {
		if len(items) > 0 {
			if err := t.k.appendTodoArchive(items); err != nil {
				return "", err
			}
		}
		f.Items = nil
		f.Reminded = nil
	} else {
		f.Items = items
	}
	// Fresh immediately, before noteTodoStep advances the counter, so a
	// reminder cannot land in the gap after this write.
	f.UpdatedStep = f.Step
	t.k.todoMem = f
	if err := t.k.saveTodo(f); err != nil {
		return "", err
	}
	if turn := t.k.liveTurn(); turn != nil {
		turn.mu.Lock()
		turn.todoTouched = true
		turn.mu.Unlock()
	}
	return formatTodoResult(items), nil
}

func todoHasOpen(items []TodoItem) bool {
	for _, it := range items {
		if todoOpen(it.Status) {
			return true
		}
	}
	return false
}

// sameTodoTask reports whether next is an update of prev. A list that shares
// no id is a new task, and an empty list clears the current one.
func sameTodoTask(prev, next []TodoItem) bool {
	if len(prev) == 0 || len(next) == 0 {
		return false
	}
	ids := make(map[string]bool, len(prev))
	for _, it := range prev {
		ids[it.ID] = true
	}
	for _, it := range next {
		if ids[it.ID] {
			return true
		}
	}
	return false
}

// archiveTodoForNewTask moves the active list aside when a new owner message
// starts. A timer wake is not a new task and leaves the list in place.
func (k *Kernel) archiveTodoForNewTask() error {
	if k == nil || k.turnIsTimer() {
		return nil
	}
	on, _ := k.todoPolicy()
	if !on {
		return nil
	}
	k.todoMu.Lock()
	defer k.todoMu.Unlock()
	f, err := k.todoStateLocked()
	if err != nil || len(f.Items) == 0 {
		return err
	}
	if err := k.appendTodoArchive(f.Items); err != nil {
		return err
	}
	f.Items = nil
	f.Reminded = nil
	k.todoMem = f
	return k.saveTodo(f)
}

func (k *Kernel) appendTodoArchive(items []TodoItem) error {
	if k == nil || k.Bot == nil || len(items) == 0 {
		return nil
	}
	return fsutil.AppendJSONL(TodoArchivePath(k.Bot), struct {
		Items []TodoItem `json:"items"`
		At    time.Time  `json:"at"`
	}{Items: append([]TodoItem(nil), items...), At: k.now()})
}

func parseTodoItems(args json.RawMessage, redact func(string) string) ([]TodoItem, error) {
	var in struct {
		Items []TodoItem `json:"items"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.Items == nil {
		return nil, errors.New("todo: items is required")
	}
	if len(in.Items) > todoMaxItems {
		return nil, fmt.Errorf("todo: at most %d items", todoMaxItems)
	}
	if redact == nil {
		redact = func(s string) string { return s }
	}
	seen := map[string]bool{}
	progress := 0
	out := make([]TodoItem, 0, len(in.Items))
	for i, it := range in.Items {
		it.Content = redact(strings.TrimSpace(it.Content))
		it.Reason = redact(strings.TrimSpace(it.Reason))
		it.Status = strings.ToLower(strings.TrimSpace(it.Status))
		if it.Content == "" {
			return nil, fmt.Errorf("todo: item %d has empty content", i+1)
		}
		if utf8.RuneCountInString(it.Content) > todoMaxRunes {
			return nil, fmt.Errorf("todo: item %d content is too long", i+1)
		}
		if utf8.RuneCountInString(it.Reason) > todoMaxRunes {
			return nil, fmt.Errorf("todo: item %d reason is too long", i+1)
		}
		switch it.Status {
		case "pending", "in_progress", "done":
		case "dropped":
			if it.Reason == "" {
				return nil, fmt.Errorf("todo: item %d is dropped without a reason", i+1)
			}
		default:
			return nil, fmt.Errorf("todo: item %d has bad status %q", i+1, it.Status)
		}
		if it.Status == "in_progress" {
			progress++
		}
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = fmt.Sprintf("t%d", i+1)
			for seen[id] {
				id += "x"
			}
		}
		if !todoIDRe.MatchString(id) {
			return nil, fmt.Errorf("todo: bad id %q", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("todo: duplicate id %q", id)
		}
		seen[id] = true
		it.ID = id
		if it.Status != "dropped" {
			it.Reason = ""
		}
		out = append(out, it)
	}
	if progress > 1 {
		return nil, errors.New("todo: at most one item can be in_progress")
	}
	return out, nil
}

func formatTodoResult(items []TodoItem) string {
	var pending, progress, done, dropped int
	for _, it := range items {
		switch it.Status {
		case "pending":
			pending++
		case "in_progress":
			progress++
		case "done":
			done++
		case "dropped":
			dropped++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "todo: %d items (%d in_progress, %d pending, %d done, %d dropped)", len(items), progress, pending, done, dropped)
	for _, it := range items {
		fmt.Fprintf(&b, "\n- [%s] %s %s", it.Status, it.ID, it.Content)
		if it.Reason != "" {
			fmt.Fprintf(&b, " (%s)", it.Reason)
		}
	}
	return b.String()
}
