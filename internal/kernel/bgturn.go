package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

const (
	defaultForkCap   = 3
	defaultForkAfter = 3 * time.Second
	forkPoll         = 300 * time.Millisecond
	forkTaskRunes    = 80
	forkReplyRunes   = 2000
	finishedCap      = 3
)

// BackgroundInfo is one background turn, as the foreground note and the web
// stream list it. A running entry has no Status or Ended; a finished one has both.
type BackgroundInfo struct {
	ID       string     `json:"id"`
	Task     string     `json:"task"`
	Steps    int        `json:"steps"`
	LastTool string     `json:"last_tool"`
	Status   string     `json:"status,omitempty"`
	Ended    *time.Time `json:"ended,omitempty"`
}

func (k *Kernel) forkCap() int {
	if k.ForkCap > 0 {
		return k.ForkCap
	}
	return defaultForkCap
}

func (k *Kernel) forkAfter() time.Duration {
	if k.ForkAfter > 0 {
		return k.ForkAfter
	}
	return defaultForkAfter
}

// Backgrounds lists the running background turns, oldest first.
func (k *Kernel) Backgrounds() []BackgroundInfo {
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	out := make([]BackgroundInfo, 0, len(k.bgs))
	for _, t := range k.bgs {
		t.mu.Lock()
		out = append(out, BackgroundInfo{ID: t.branch, Task: t.task, Steps: t.replies, LastTool: t.lastTool})
		t.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FinishedBackgrounds lists the most recently finished background turns, newest
// first, capped at three. It returns a copy the caller may modify.
func (k *Kernel) FinishedBackgrounds() []BackgroundInfo {
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	out := make([]BackgroundInfo, len(k.finished))
	copy(out, k.finished)
	return out
}

// ownerWaiting reports an owner message in the inbox other than the one t is handling.
// Timers, webhooks, and other bots never cause a fork.
func (k *Kernel) ownerWaiting(t *turn) bool {
	if k.Inbox == nil {
		return false
	}
	if err := k.Inbox.Reload(); err != nil {
		return false
	}
	owners := k.Bot.Config().Owners
	for _, ev := range k.Inbox.Pending() {
		if ev.ID == "" || ev.ID == t.turnEvent {
			continue
		}
		if link.OwnerTrusted(ev.Source, ev.Sender, owners) {
			return true
		}
	}
	return false
}

// tryFork moves the running turn t to the background when the owner is waiting,
// the turn is past the threshold, and the cap allows another background.
// It writes the fork marker and closes t.forked. It is safe to call repeatedly.
func (k *Kernel) tryFork(t *turn) bool {
	if !k.AllowFork || t == nil || t.forked == nil {
		return false
	}
	t.forkMu.Lock()
	defer t.forkMu.Unlock()
	return k.tryForkLocked(t)
}

// tryForkLocked is tryFork with t.forkMu held.
func (k *Kernel) tryForkLocked(t *turn) bool {
	t.mu.Lock()
	open := t.branch == "" && !t.finished && !t.answered
	eligible := open && (t.tools > 0 || k.now().Sub(t.started) >= k.forkAfter())
	capped := open && k.foregroundCapped(t)
	t.mu.Unlock()
	// A turn past the foreground cap moves to the background on its own; any other
	// turn forks only for an owner message.
	if !capped && (!eligible || !k.ownerWaiting(t)) {
		return false
	}
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	if len(k.bgs) >= k.forkCap() {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.branch != "" || t.finished || t.answered {
		return false
	}
	id := newBranchID()
	// Holding t.mu keeps the turn's own writes out until the marker is down.
	if _, err := k.Bot.AppendFork(id, t.fromSeq, t.task); err != nil {
		return false
	}
	t.branch = id
	k.adoptForeground(id)
	if k.bgs == nil {
		k.bgs = map[string]*turn{}
	}
	k.bgs[id] = t
	t.capFork = capped
	t.segments = 1
	k.writeForkMeta(t, "running")
	close(t.forked)
	return true
}

// finishBackground closes a background turn: fork_end, meta, and a completion event.
func (k *Kernel) finishBackground(t *turn, produced []provider.Message, runErr error) {
	t.mu.Lock()
	id, steps := t.branch, t.replies
	segments, stopReason := t.segments, t.stopReason
	t.mu.Unlock()
	status := "done"
	if runErr != nil {
		status = "failed"
		if t.cancelled() {
			status = "cancelled"
		}
	}
	// The branch's own memory writes happened after Handle returned; save them now.
	if st, err := k.Bot.LoadState(); err == nil {
		k.saveMemOwn(t, migrate(st))
	}
	_, _ = k.Bot.AppendForkEnd(id, status, steps)
	k.writeForkMeta(t, status)
	k.bgMu.Lock()
	delete(k.bgs, id)
	ended := k.now()
	k.finished = append([]BackgroundInfo{{ID: id, Task: t.task, Steps: steps, Status: status, Ended: &ended}}, k.finished...)
	if len(k.finished) > finishedCap {
		k.finished = k.finished[:finishedCap]
	}
	k.bgMu.Unlock()
	if k.Inbox == nil {
		return
	}
	text := fmt.Sprintf("后台任务 %s 结束：%s，%d 段 %d 步。任务：%s", id, status, segments, steps, t.task)
	if runErr != nil {
		text += "\n错误：" + clipRunes(runErr.Error(), 300)
	}
	if stopReason != "" {
		text += "\n卡在这里：" + stopReason + "。要我接着做，回一句「继续」就行。"
	}
	t.mu.Lock()
	var unread []string
	for _, c := range t.ctrl {
		if c.State == "pending" {
			c.State = "dropped"
			unread = append(unread, c.Kind+"："+c.Text)
		}
	}
	t.mu.Unlock()
	if len(unread) > 0 {
		text += "\n没来得及处理的控制：" + strings.Join(unread, "；")
	}
	if last := lastText(produced); last != "" {
		text += "\n最后的回复：\n" + clipRunes(last, forkReplyRunes)
	}
	_ = k.Inbox.Post(link.Event{
		ID:        "ev_" + id + "_end",
		Source:    link.SourceSubagent,
		Type:      "fork_end",
		Content:   text,
		Sender:    id,
		Time:      k.now(),
		Untrusted: true,
	})
}

// backgroundNote is the line the foreground sees before the owner's message.
func backgroundNote(list []BackgroundInfo) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<background_tasks>\n后台任务正在执行（不要重复做这些事；要改它们用 steer/queue/interrupt/cancel）：")
	for _, bg := range list {
		fmt.Fprintf(&b, "\n- %s：%s，已跑 %d 步", bg.ID, bg.Task, bg.Steps)
		if bg.LastTool != "" {
			fmt.Fprintf(&b, "，最近在用 %s", bg.LastTool)
		}
	}
	b.WriteString("\n</background_tasks>")
	return b.String()
}

func (k *Kernel) writeForkMeta(t *turn, status string) {
	dir := filepath.Join(filepath.Dir(k.Bot.OutboxPath()), "agents", t.branch)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"id": t.branch, "mode": "fork-turn", "status": status, "task": t.task,
		"from": t.fromSeq, "steps": t.replies, "started": t.started, "updated": k.now(),
	})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o600)
}

func newBranchID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "bg_" + hex.EncodeToString(b[:])
}

func lastText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && len(msgs[i].ToolCalls) == 0 && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

var _ = bot.ForkKindStart

// QueueCap is how many queued items (owner queue controls and conflict waits) a
// kernel holds at once. Past it the foreground answers that the queue is full.
const QueueCap = 5

// Control is one message sent to a background: steer, queue, interrupt, or a
// foreground call queued behind it on a resource conflict ("conflict").
// State is pending until the background reads it between steps, then applied.
type Control struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	State      string `json:"state"`
	Background string `json:"background"`
	Resource   string `json:"resource,omitempty"`
}

func (c *Control) message() string {
	switch c.Kind {
	case "steer":
		return "[主人的补充] " + c.Text
	case "queue":
		return "[排队] 做完当前的事后再做：" + c.Text
	case "interrupt":
		return "[打断] 停下当前的做法，先处理：" + c.Text
	case "conflict":
		return "[排队] 前台要做的事和你正在动的 " + c.Resource + " 冲突，已排到你之后。做完当前的事再做：" + c.Text
	}
	return c.Text
}

func (c *Control) queued() bool {
	return c.State == "pending" && (c.Kind == "queue" || c.Kind == "conflict")
}

// QueueItem is one queued item with why it waits and its place in line.
// Reason is "conflict" (Background and Resource name the holder) or "slot"
// (all background slots are taken; an owner message waits in the inbox).
type QueueItem struct {
	ID         string `json:"id"`
	Reason     string `json:"reason"`
	Background string `json:"background,omitempty"`
	Resource   string `json:"resource,omitempty"`
	Name       string `json:"name,omitempty"`
	Text       string `json:"text"`
	Position   int    `json:"position"`
}

// resourceName is the short label a resource shows in the queue: the base name
// of a file or directory, or the repository name.
func resourceName(res string) string {
	_, rest, ok := strings.Cut(res, ":")
	if !ok {
		rest = res
	}
	rest = strings.TrimRight(rest, "/")
	if i := strings.LastIndexByte(rest, '/'); i >= 0 {
		rest = rest[i+1:]
	}
	return rest
}

var ctrlSeq atomic.Int64

// Controls lists every control sent to running backgrounds, pending and applied.
func (k *Kernel) Controls() []Control {
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	var out []Control
	for _, id := range k.bgIDsLocked() {
		t := k.bgs[id]
		t.mu.Lock()
		for _, c := range t.ctrl {
			out = append(out, *c)
		}
		t.mu.Unlock()
	}
	return out
}

func (k *Kernel) bgIDsLocked() []string {
	ids := make([]string, 0, len(k.bgs))
	for id := range k.bgs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Queue lists queued items in order: pending queue and conflict controls by id,
// then owner messages waiting for a slot while every slot is taken.
func (k *Kernel) Queue() []QueueItem {
	k.bgMu.Lock()
	var out []QueueItem
	full := len(k.bgs) >= k.forkCap()
	for _, id := range k.bgIDsLocked() {
		t := k.bgs[id]
		t.mu.Lock()
		for _, c := range t.ctrl {
			if c.queued() {
				out = append(out, QueueItem{ID: c.ID, Reason: "conflict", Background: c.Background, Resource: c.Resource, Name: resourceName(c.Resource), Text: c.Text})
				if c.Kind == "queue" {
					out[len(out)-1].Reason = "queue"
				}
			}
		}
		t.mu.Unlock()
	}
	k.bgMu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if full && k.Inbox != nil && k.Inbox.Reload() == nil {
		owners := k.Bot.Config().Owners
		cur := k.curTurn()
		for _, ev := range k.Inbox.Pending() {
			if ev.ID == "" || ev.ID == cur.turnEvent || !link.OwnerTrusted(ev.Source, ev.Sender, owners) {
				continue
			}
			out = append(out, QueueItem{ID: ev.ID, Reason: "slot", Text: ev.Content})
		}
	}
	for i := range out {
		out[i].Position = i + 1
	}
	return out
}

// QueueFull reports that the queue is at QueueCap, so a new item is refused.
func (k *Kernel) QueueFull() bool { return len(k.Queue()) >= QueueCap }

// controlBackground sends steer, queue, interrupt, or cancel to a running
// background, or cancels a pending queued control by its id ("cancel_queued").
func (k *Kernel) controlBackground(id, kind, text string) (string, error) {
	if kind == "cancel_queued" {
		return k.cancelQueued(text)
	}
	k.bgMu.Lock()
	t := k.bgs[id]
	k.bgMu.Unlock()
	if t == nil {
		return "", fmt.Errorf("background %s is not running", id)
	}
	text = strings.TrimSpace(text)
	switch kind {
	case "cancel":
		if t.cancelRun != nil {
			t.cancelRun()
		}
		return "cancel " + id, nil
	case "steer", "queue", "interrupt":
	default:
		return "", fmt.Errorf("background: unknown control %q", kind)
	}
	if text == "" {
		return "", fmt.Errorf("background: text is empty")
	}
	if kind == "queue" && k.QueueFull() {
		return "", fmt.Errorf("队列已满（%d 个），没有排入", QueueCap)
	}
	c := &Control{ID: fmt.Sprintf("q%d", ctrlSeq.Add(1)), Kind: kind, Text: text}
	if !t.queueCtrl(c) {
		return "", fmt.Errorf("background %s has finished", id)
	}
	return fmt.Sprintf("%s %s %s pending: applied after the current step", kind, id, c.ID), nil
}

// cancelQueued drops a pending control so the background never reads it.
func (k *Kernel) cancelQueued(cid string) (string, error) {
	cid = strings.TrimSpace(cid)
	k.bgMu.Lock()
	for _, t := range k.bgs {
		t.mu.Lock()
		for i, c := range t.ctrl {
			if c.ID == cid && c.State == "pending" {
				t.ctrl = append(t.ctrl[:i], t.ctrl[i+1:]...)
				t.mu.Unlock()
				k.bgMu.Unlock()
				return "cancelled " + cid, nil
			}
		}
		t.mu.Unlock()
	}
	k.bgMu.Unlock()
	// A slot item is an owner message still in the inbox.
	for _, q := range k.Queue() {
		if q.ID == cid && q.Reason == "slot" {
			if err := k.Inbox.Ack(cid); err != nil {
				return "", err
			}
			return "cancelled " + cid, nil
		}
	}
	return "", fmt.Errorf("queued item %s is not pending", cid)
}

// conflictingBackground returns a running background, other than cur, that has
// written res. The policy: a foreground write to the same file, repository, or
// working directory waits behind that background; anything else runs in parallel.
func (k *Kernel) conflictingBackground(cur *turn, res string) *turn {
	k.bgMu.Lock()
	defer k.bgMu.Unlock()
	ids := make([]string, 0, len(k.bgs))
	for id := range k.bgs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if t := k.bgs[id]; t != cur && t.touches(res) {
			return t
		}
	}
	return nil
}

// CancelBackgrounds cancels every running background, for a bot being deleted or renamed.
func (k *Kernel) CancelBackgrounds() {
	k.bgMu.Lock()
	ts := make([]*turn, 0, len(k.bgs))
	for _, t := range k.bgs {
		ts = append(ts, t)
	}
	k.bgMu.Unlock()
	for _, t := range ts {
		if t.cancelRun != nil {
			t.cancelRun()
		}
	}
}

// ForkCapValue is the background slot cap in effect.
func (k *Kernel) ForkCapValue() int { return k.forkCap() }

// foregroundCapped reports a main-line turn past the foreground cap: ForegroundMaxTools
// tool calls (default 4) or ForegroundMaxTime (default 20s). A negative value turns
// that limit off. The caller holds t.mu.
func (k *Kernel) foregroundCapped(t *turn) bool {
	maxTools, maxTime := k.ForegroundMaxTools, k.ForegroundMaxTime
	if maxTools == 0 {
		maxTools = 4
	}
	if maxTime == 0 {
		maxTime = 20 * time.Second
	}
	if maxTools > 0 && t.tools >= maxTools {
		return true
	}
	return maxTime > 0 && k.now().Sub(t.started) >= maxTime
}

// capNotice is the main-line reply when a turn moves to the background because it
// passed the foreground cap.
func capNotice(id string) string {
	return fmt.Sprintf("这件事比较长，已转到后台 %s 继续做，做完告诉你。", id)
}
