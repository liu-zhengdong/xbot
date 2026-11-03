// Package link is the only way a bot touches the outside world.
//
// A Link has two directions:
//   - Outbound: Tools the model may call (shell, files, and later Slack or HTTP).
//   - Inbound: external happenings become inbox.Events posted to the bot's inbox.
//
// The kernel knows the inbox and the tools. It does not know Slack, MCP, or the terminal.
// Proactivity is a timer Link that emits events. M1 defines Emitter only; cron is M2.
//
// Kernel rules enforced here and in the kernel:
//   - inbox.NewEvent marks content untrusted. The kernel, not the producer, decides
//     whether an owner on the terminal link is trusted.
//   - External links are installed per bot or globally; native organs are always present.
package link

import (
	"context"

	"xbot/internal/inbox"
	"xbot/internal/tools"
)

const (
	NameMemory   = "memory"
	NamePersona  = "persona"
	NameContext  = "context"
	NameAgents   = "agents"
	NameSkills   = "skills"
	NameRunner   = "runner"
	NameWebhook  = "webhook"
	NameWatch    = "watch"
	NamePeers    = "peers"
	NameTelegram = "telegram"
	NameLark     = "lark"
	NameDesktop  = "desktop"
	NameBrowser  = "browser"
)

// Link is a channel between one bot and the outside world.
// Tools is the outbound direction. Inbound delivery is an Event posted to the inbox.
type Link interface {
	Name() string
	Tools() []tools.Tool
}

// Emitter is an inbound link that wakes the bot over time.
// The timer link implements Emitter.
type Emitter interface {
	Link
	Run(ctx context.Context, emit func(inbox.Event) error) error
}
