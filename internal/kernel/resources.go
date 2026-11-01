package kernel

import (
	"context"
	"errors"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

// BotResources survives replaceable kernels; the process Manager owns MCP.
// The route protects short filesystem operations across an actual directory move.
type BotResources struct {
	MCP     *mcp.BotRef
	Jobs    *tools.Jobs
	route   sync.RWMutex
	current *bot.Bot
	box     *inbox.Inbox
	closed  bool
	ctx     context.Context
	manager *mcp.Manager
}

func NewBotResources(ctx context.Context, b *bot.Bot, manager *mcp.Manager) (*BotResources, error) {
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return nil, err
	}
	r := &BotResources{current: b, box: box, ctx: ctx, manager: manager}
	ref, err := manager.Bind(mcp.Binding{Key: b.Name(), ReadConfig: r.readMCPConfig, Notify: r.postMCP})
	if err != nil {
		return nil, err
	}
	r.MCP = ref
	return r, nil
}

func (r *BotResources) readMCPConfig() ([]mcp.Spec, error) {
	r.route.RLock()
	defer r.route.RUnlock()
	if r.closed {
		return nil, errors.New("bot resources closed")
	}
	specs, err := link.LoadMCPAll(r.current.GlobalMCPPath(), r.current.MCPPath())
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

func (r *BotResources) MCPStatus(spec link.MCPServer) link.MCPStatus {
	for _, current := range r.MCP.Specs() {
		if sameMCPSpec(current, spec) {
			return r.MCP.Status(spec.Name)
		}
	}
	return link.MCPStatus{State: "configured"}
}
