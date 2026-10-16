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
)

// BackgroundInfo is one running background turn, as the foreground note lists it.
type BackgroundInfo struct {
	ID       string
	Task     string
	Steps    int
	LastTool string
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
		out = append(out, BackgroundInfo{ID: t.branch, Task: t.task, Steps: t.steps, LastTool: t.lastTool})
		t.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
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
	eligible := t.branch == "" && !t.finished && !t.answered && (t.tools > 0 || k.now().Sub(t.started) >= k.forkAfter())
	t.mu.Unlock()
	if !eligible || !k.ownerWaiting(t) {
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
	if k.bgs == nil {
		k.bgs = map[string]*turn{}
	}
	k.bgs[id] = t
	k.writeForkMeta(t, "running")
	close(t.forked)
	return true
}

// finishBackground closes a background turn: fork_end, meta, and a completion event.
func (k *Kernel) finishBackground(t *turn, produced []provider.Message, runErr error) {
	t.mu.Lock()
	id, steps := t.branch, t.steps
	t.mu.Unlock()
	status := "done"
	if runErr != nil {
		status = "failed"
		if t.cancelled() {
			status = "cancelled"
		}
	}
	_, _ = k.Bot.AppendForkEnd(id, status, steps)
	k.writeForkMeta(t, status)
	k.bgMu.Lock()
	delete(k.bgs, id)
	k.bgMu.Unlock()
	if k.Inbox == nil {
		return
	}
	text := fmt.Sprintf("后台任务 %s 结束：%s，%d 步。任务：%s", id, status, steps, t.task)
	if runErr != nil {
		text += "\n错误：" + clipRunes(runErr.Error(), 300)
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
	b.WriteString("后台任务正在执行（不要重复做这些事；要改它们用 steer/queue/interrupt/cancel）：")
	for _, bg := range list {
		fmt.Fprintf(&b, "\n- %s：%s，已跑 %d 步", bg.ID, bg.Task, bg.Steps)
		if bg.LastTool != "" {
			fmt.Fprintf(&b, "，最近在用 %s", bg.LastTool)
		}
	}
	return b.String()
}

func (k *Kernel) writeForkMeta(t *turn, status string) {
	dir := filepath.Join(filepath.Dir(k.Bot.OutboxPath()), "agents", t.branch)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"id": t.branch, "mode": "fork-turn", "status": status, "task": t.task,
		"from": t.fromSeq, "steps": t.steps, "started": t.started, "updated": k.now(),
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
