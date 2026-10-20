package exec

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// SubPersona is the system prompt for a brief subagent.
// Fork mode keeps the main bot's context instead of this text.
const SubPersona = "You are a subagent working for the main bot. Complete the assigned task. Do not spawn further subagents. If you need a decision, reply with a line that starts with BLOCKED. Otherwise finish with a short result summary."

// reportBytes caps the summary a finished subagent hands back. A typed
// report has a few sections (what changed, test results, what is left), so it
// gets more room than a one-line status.
const reportBytes = 4096

type job struct {
	id            string
	task          string
	mode          string
	kind          string
	persona       string
	fork          []provider.Message
	maxSteps      int
	timeout       time.Duration
	progressEvery time.Duration
	dir           string
	resource      string
	provider      provider.Provider
	tools         []tools.Tool
	approve       agent.Approver
	parent        *Local

	mu         sync.Mutex
	stepCancel context.CancelFunc
	interrupt  string
	cancelReq  bool
	ctrl       chan Control
	held       []Control
	steps      int
}

func runLoop(j *job) {
	ctx, cancel := context.WithTimeout(context.Background(), j.timeout)
	defer cancel()
	if err := os.MkdirAll(j.dir, 0700); err != nil {
		j.finish(statusFailed, EvFailed, err.Error())
		return
	}
	msgs := initialMessages(j)
	for _, m := range msgs {
		if err := appendEntry(j.dir, bot.Entry{Time: time.Now().UTC(), Message: m}); err != nil {
			j.finish(statusFailed, EvFailed, err.Error())
			return
		}
	}
	_ = writeMeta(j.dir, Meta{ID: j.id, Status: statusRunning, Task: j.task, Mode: j.mode, Type: j.kind})
	stepsLeft := j.maxSteps
	var queued []string
	var lastProgress time.Time
	for {
		if ctx.Err() != nil {
			j.finish(statusFromCtx(ctx), EvFailed, statusText(ctx))
			return
		}
		if stop, _ := j.drain(&msgs, &stepsLeft, &queued); stop {
			j.finish(statusCancelled, EvFailed, "cancelled")
			return
		}
		if stepsLeft <= 0 {
			if len(queued) > 0 {
				if !j.startQueued(&msgs, &stepsLeft, &queued) {
					return
				}
				continue
			}
			j.finish(statusFailed, EvFailed, "max steps reached")
			return
		}
		stepCtx, stepCancel := context.WithCancel(ctx)
		j.mu.Lock()
		j.stepCancel = stepCancel
		j.mu.Unlock()
		var appendErr error
		produced, err := agent.Run(stepCtx, j.provider, j.tools, msgs, agent.Options{
			MaxSteps: 1,
			OneRound: true,
			Approve:  j.approve,
			OnMessage: func(m provider.Message) {
				if appendErr != nil {
					return
				}
				appendErr = appendEntry(j.dir, bot.Entry{Time: time.Now().UTC(), Message: m})
			},
		})
		j.mu.Lock()
		j.stepCancel = nil
		intr := j.interrupt
		j.interrupt = ""
		cancelled := j.cancelReq
		j.mu.Unlock()
		stepCancel()
		if appendErr != nil {
			j.finish(statusFailed, EvFailed, appendErr.Error())
			return
		}
		if cancelled {
			j.finish(statusCancelled, EvFailed, "cancelled")
			return
		}
		prior := len(msgs)
		msgs = append(msgs, produced...)
		// A round that attached a newer image set replaces the one before it.
		// agent.Run drops the older set on its own copy of the conversation, so
		// the loop has to drop it here or every round keeps re-sending images.
		if hasImages(produced) {
			dropImages(msgs[:prior])
		}
		// A reply with no tool calls consumed the images. Drop them before a
		// queued task, an interrupt, or the next round, so the job can view
		// another image. A round that is still calling tools keeps its images.
		if replyClosed(produced) {
			dropImages(msgs)
		}
		if intr != "" {
			if err := j.appendUser(&msgs, intr); err != nil {
				j.finish(statusFailed, EvFailed, err.Error())
				return
			}
			stepsLeft = j.maxSteps
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				j.finish(statusFromCtx(ctx), EvFailed, statusText(ctx))
				return
			}
			j.finish(statusFailed, EvFailed, err.Error())
			return
		}
		j.steps++
		stepsLeft--
		_ = writeMeta(j.dir, Meta{ID: j.id, Status: statusRunning, Task: j.task, Mode: j.mode, Type: j.kind, Steps: j.steps})
		stop, steered := j.drain(&msgs, &stepsLeft, &queued)
		if stop {
			j.finish(statusCancelled, EvFailed, "cancelled")
			return
		}
		last := lastAssistant(produced)
		if len(last.ToolCalls) == 0 && last.Role != "" {
			text := strings.TrimSpace(last.Content)
			if strings.HasPrefix(text, "BLOCKED") {
				j.parent.emit(Event{AgentID: j.id, Kind: EvBlocked, Text: clipText(text, 2048)})
				if err := j.wait(ctx); err != nil {
					j.finish(statusFromCtx(ctx), EvFailed, statusText(ctx))
					return
				}
				continue
			}
			if steered {
				continue
			}
			if len(queued) > 0 {
				if !j.startQueued(&msgs, &stepsLeft, &queued) {
					return
				}
				continue
			}
			summary := text
			if summary == "" {
				summary = "done"
			}
			j.finish(statusDone, EvDone, summary)
			return
		}
		if j.progressEvery < 0 || time.Since(lastProgress) >= j.progressEvery {
			j.parent.emit(Event{AgentID: j.id, Kind: EvProgress, Text: progressText(produced)})
			lastProgress = time.Now()
		}
	}
}

func (j *job) startQueued(msgs *[]provider.Message, stepsLeft *int, queued *[]string) bool {
	text := (*queued)[0]
	*queued = (*queued)[1:]
	if err := j.appendUser(msgs, text); err != nil {
		j.finish(statusFailed, EvFailed, err.Error())
		return false
	}
	*stepsLeft = j.maxSteps
	return true
}

func (j *job) wait(ctx context.Context) error {
	if len(j.held) > 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c := <-j.ctrl:
		j.held = append(j.held, c)
		return nil
	}
}

func (j *job) drain(msgs *[]provider.Message, stepsLeft *int, queued *[]string) (stop bool, steered bool) {
	for len(j.held) > 0 {
		c := j.held[0]
		j.held = j.held[1:]
		if j.applyOne(c, msgs, stepsLeft, queued) {
			return true, steered
		}
		if c.Kind == KindSteer || c.Kind == KindInterrupt {
			steered = true
		}
	}
	for {
		select {
		case c := <-j.ctrl:
			if j.applyOne(c, msgs, stepsLeft, queued) {
				return true, steered
			}
			if c.Kind == KindSteer || c.Kind == KindInterrupt {
				steered = true
			}
		default:
			return false, steered
		}
	}
}

func (j *job) applyOne(c Control, msgs *[]provider.Message, stepsLeft *int, queued *[]string) bool {
	switch c.Kind {
	case KindCancel:
		return true
	case KindSteer:
		if msgs != nil {
			_ = j.appendUser(msgs, c.Text)
		}
	case KindInterrupt:
		if msgs != nil {
			_ = j.appendUser(msgs, c.Text)
		}
		if stepsLeft != nil {
			*stepsLeft = j.maxSteps
		}
	case KindQueue:
		if queued != nil {
			*queued = append(*queued, c.Text)
		}
	}
	return false
}

func (j *job) appendUser(msgs *[]provider.Message, text string) error {
	m := provider.Message{Role: provider.RoleUser, Content: text}
	if err := appendEntry(j.dir, bot.Entry{Time: time.Now().UTC(), Message: m}); err != nil {
		return err
	}
	*msgs = append(*msgs, m)
	return nil
}

func (j *job) finish(status, kind, text string) {
	text = clipText(strings.TrimSpace(text), reportBytes)
	if text == "" {
		text = status
	}
	_ = writeMeta(j.dir, Meta{ID: j.id, Status: status, Summary: text, Task: j.task, Mode: j.mode, Type: j.kind, Steps: j.steps})
	j.parent.emit(Event{AgentID: j.id, Kind: kind, Text: text})
}

func initialMessages(j *job) []provider.Message {
	task := provider.Message{Role: provider.RoleUser, Content: "Task:\n" + j.task}
	if j.mode == ModeFork && len(j.fork) > 0 {
		return append(copyMessages(j.fork), task)
	}
	persona := SubPersona
	if strings.TrimSpace(j.persona) != "" {
		persona = j.persona
	}
	return []provider.Message{
		{Role: provider.RoleSystem, Content: persona},
		{Role: provider.RoleUser, Content: j.task},
	}
}

func replyClosed(msgs []provider.Message) bool {
	last := lastAssistant(msgs)
	return last.Role != "" && len(last.ToolCalls) == 0
}

func dropImages(msgs []provider.Message) {
	for i := range msgs {
		msgs[i].Images = nil
	}
}

func hasImages(msgs []provider.Message) bool {
	for _, m := range msgs {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}

func lastAssistant(msgs []provider.Message) provider.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant {
			return msgs[i]
		}
	}
	return provider.Message{}
}

func progressText(msgs []provider.Message) string {
	if m := lastAssistant(msgs); strings.TrimSpace(m.Content) != "" {
		return clipText(m.Content, 240)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleTool && strings.TrimSpace(msgs[i].Content) != "" {
			return clipText(msgs[i].Name+": "+msgs[i].Content, 240)
		}
	}
	return "working"
}

func clipText(s string, n int) string {
	s, _ = clip.Bytes(strings.TrimSpace(s), n)
	return s
}

func statusFromCtx(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return statusFailed
	}
	return statusCancelled
}

func statusText(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed out"
	}
	return "cancelled"
}
