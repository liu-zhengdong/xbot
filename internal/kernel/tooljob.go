package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/tools"
)

// callTurnKey keeps the original execution identity for file-read receipts.
// The ordinary turn value is frozen only for sender, permissions and routing.
type callTurnKey struct{}

// callReplyGenKey identifies the next reply when a tool call was issued.
type callReplyGenKey struct{}

func (k *Kernel) callTurn(ctx context.Context) *turn {
	if t, ok := ctx.Value(callTurnKey{}).(*turn); ok {
		return t
	}
	return k.turnFor(ctx)
}

func (k *Kernel) runToolCall(ctx context.Context, t tools.Tool, args json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
	// Inspecting/cancelling Jobs must not itself require a free Job slot.
	base := t
	if rt, ok := t.(reviewedTool); ok {
		base = rt.Tool
	}
	switch base.(type) {
	case shellOutputTool, shellStatusTool, shellCancelTool:
		return run(ctx)
	}
	jobs := k.shellJobs()
	if jobs == nil {
		if k.Bot != nil {
			return tools.CallResult{Err: errors.New("tool job storage unavailable")}
		}
		return run(ctx)
	}
	wait, bg := tools.CallWait(t, args)
	summary := agent.ToolSummary(ctx)
	parentCtx := ctx
	parentScope := scopeOf(ctx)
	owner := k.turnFor(ctx)
	owner.mu.Lock()
	frozen := &turn{turnSender: owner.turnSender, turnKind: owner.turnKind, turnTrusted: owner.turnTrusted, turnSource: owner.turnSource, turnHops: owner.turnHops, turnChain: owner.turnChain, branch: owner.branch, fromSeq: owner.fromSeq}
	replyGen := owner.replyGen
	owner.mu.Unlock()
	return jobs.RunCall(ctx, tools.CallRequest{Name: t.Name(), Command: summary, Dir: k.cwdFor(ctx), Wait: wait, Background: bg, Run: run, Finalize: func(job tools.Job, res tools.CallResult) (tools.Job, error) {
		return exec.ToolJobReport(k.workspace(), job, res)
	},
		Prepare: func(ctx context.Context, id string) (context.Context, func()) {
			// Copy the raw cwd so the shell can still report a missing directory.
			k.noteScope(id, k.readScope(parentScope))
			ctx = context.WithValue(ctx, cwdScopeKey{}, id)
			ctx = context.WithValue(ctx, callTurnKey{}, owner)
			ctx = context.WithValue(ctx, callReplyGenKey{}, replyGen)
			ctx = context.WithValue(ctx, turnKey{}, frozen)
			return ctx, func() {
				st, err := jobs.Status(id)
				if err == nil && !st.Background && st.Status == "done" {
					k.storeScope(scopeOf(parentCtx), k.readScope(id))
				}
				k.dropScope(id)
			}
		}})
}

// A finished call always posts the same ordinary inbox event, busy or idle.
// Job.Notified is transport acknowledgement only, never a claim of user delivery.
func (k *Kernel) onToolDone(job tools.Job) {
	if k == nil || k.Inbox == nil || job.Status == "running" || job.ID == "" || job.Notified {
		return
	}
	jobs := k.liveShell()
	if jobs == nil {
		return
	}
	posted := false
	err := jobs.Deliver(job.ID, func(job tools.Job, _ tools.CallResult) error {
		text := job.Result
		if text == "" {
			text = "工具 " + job.Tool + "（" + job.ID + "）：" + job.Status + "，输出 " + job.Output
		}
		err := k.Inbox.Post(link.Event{ID: "shelljob_" + job.ID, Source: "shell", Type: tools.JobEventType, Sender: job.ID, Content: text, Images: job.Images, Time: k.now(), Untrusted: true})
		posted = err == nil
		return err
	})
	if posted && err == nil && k.Bot != nil {
		if wake, ok := wakers.Load(k.Bot.Root); ok {
			wake.(func(*bot.Bot))(k.Bot)
		}
	}
}

func (k *Kernel) liveShell() *tools.Jobs {
	if k == nil {
		return nil
	}
	k.shellMu.Lock()
	defer k.shellMu.Unlock()
	return k.shell
}

func (k *Kernel) flushToolJobs() {
	jobs := k.liveShell()
	if jobs == nil {
		return
	}
	for _, job := range jobs.List() {
		if !job.Notified && (job.Background || job.Status == "lost") && job.Status != "running" {
			k.onToolDone(job)
		}
	}
}
