package kernel

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

// BotResources survives replaceable kernels; the process Manager owns MCP.
// The route protects short filesystem operations across an actual directory move.
type BotResources struct {
	MCP         *mcp.BotRef
	Jobs        *tools.Jobs
	route       sync.RWMutex
	current     *bot.Bot
	box         *inbox.Inbox
	closed      bool
	ctx         context.Context
	manager     *mcp.Manager
	Rename      func(*Kernel, bot.ProfileChange) (bot.ProfileChangeResult, error)
	Delete      func(context.Context, string) error
	Redact      func(string) string
	now         func() time.Time
	closeOnce   sync.Once
	stopContext func() bool
}

func NewBotResources(ctx context.Context, b *bot.Bot, manager *mcp.Manager, now ...func() time.Time) (*BotResources, error) {
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return nil, err
	}
	r := &BotResources{current: b, box: box, ctx: ctx, manager: manager, now: time.Now}
	if len(now) > 0 {
		r.now = now[0]
	}
	r.Redact = func(text string) string { return config.RedactHeld(bot.RedactText(text)) }
	r.Jobs = tools.NewJobs(filepath.Join(bot.Dir(b.Root, b.Name()), "shell-jobs"))
	r.Jobs.Redact = r.Redact
	r.Jobs.Forms = func() []string { return append(vault.RegisteredForms(), config.HeldForms()...) }
	r.Jobs.Now = r.now
	r.Jobs.OnDone = r.deliverToolDone
	if err := r.Jobs.Load(); err != nil {
		return nil, err
	}
	ref, err := manager.Bind(mcp.Binding{Key: b.Name(), ReadConfig: r.readMCPConfig, Notify: r.postMCP})
	if err != nil {
		r.Jobs.Close()
		return nil, err
	}
	r.MCP = ref
	r.route.Lock()
	r.stopContext = context.AfterFunc(ctx, r.Close)
	r.route.Unlock()
	return r, nil
}

func (r *BotResources) readMCPConfig() ([]mcp.Spec, error) {
	r.route.RLock()
	defer r.route.RUnlock()
	if r.closed {
		return nil, errors.New("bot resources closed")
	}
	specs, err := mcp.LoadMCPAll(mcp.Options{IsProviderKey: config.ProviderKey}, r.current.GlobalMCPPath(), r.current.MCPPath())
	if err != nil {
		return nil, err
	}
	out := make([]mcp.Spec, 0, len(specs))
	for _, spec := range specs {
		if !link.RetiredServer(spec.Name) {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (r *BotResources) postMCP(server, text string) error {
	r.route.RLock()
	if r.closed {
		r.route.RUnlock()
		return errors.New("bot resources closed")
	}
	b := r.current
	ev := link.NewEvent("mcp:"+server, link.TypeMessage, text, "mcp:"+server, time.Now())
	ev.Untrusted = true
	err := r.box.Post(ev)
	r.route.RUnlock()
	if err == nil && r.ctx.Err() == nil {
		if wake, ok := wakers.Load(b.Root); ok {
			wake.(func(*bot.Bot))(b)
		}
	}
	return err
}

func (r *BotResources) MCPStatus(spec mcp.Spec) mcp.Status {
	for _, current := range r.MCP.Specs() {
		if mcp.SameSpec(current, spec) {
			return r.MCP.Status(spec.Name)
		}
	}
	return mcp.Status{State: "configured"}
}

// Close is only for the host or actual deletion, never a borrowing Kernel.
func (r *BotResources) Close() {
	r.closeOnce.Do(func() {
		r.Jobs.Close()
		r.route.Lock()
		r.closed = true
		stop := r.stopContext
		r.stopContext = nil
		r.route.Unlock()
		if stop != nil {
			stop()
		}
		if r.MCP != nil {
			_ = r.manager.Unbind(r.MCP)
		}
	})
}

func (r *BotResources) deliverToolDone(job tools.Job) {
	if job.Status == "running" || job.ID == "" || job.Notified {
		return
	}
	posted := false
	var b *bot.Bot
	err := r.Jobs.Deliver(job.ID, func(job tools.Job, _ tools.CallResult) error {
		r.route.RLock()
		defer r.route.RUnlock()
		if r.closed {
			return errors.New("bot resources closed")
		}
		b = r.current
		text := job.Result
		if text == "" {
			text = "工具 " + job.Tool + "（" + job.ID + "）：" + job.Status + "，输出 " + job.Output
		}
		err := r.box.Post(link.Event{ID: "shelljob_" + job.ID, Source: "shell", Type: tools.JobEventType, Sender: job.ID, Content: text, Images: job.Images, Time: r.now(), Untrusted: true})
		posted = err == nil
		return err
	})
	if posted && err == nil && r.ctx.Err() == nil {
		if wake, ok := wakers.Load(b.Root); ok {
			wake.(func(*bot.Bot))(b)
		}
	}
}
