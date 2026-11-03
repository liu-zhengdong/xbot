package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/notify"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// modelTools is what one model request can run: every registered tool, with
// only the hot ones and the tools door declared in tools[] (ADR 0035).
func (k *Kernel) modelTools() ([]tools.Tool, error) {
	r := k.toolRegistry()
	list := make([]tools.Tool, 0, len(r.entries)+1)
	for _, e := range r.entries {
		list = append(list, reviewedTool{Tool: e.tool, k: k, undeclared: e.expose != exposeHot})
	}
	list = append(list, reviewedTool{Tool: toolDoor{k}, k: k})
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	return list, nil
}

// mountedTools is every registered tool not waiting for links load. A
// subagent gets it minus the tools that hand work out (subagentBase), all
// declared: a subagent has no tools door.
func (k *Kernel) mountedTools() ([]tools.Tool, error) {
	return k.toolRegistry().loaded(), nil
}

// selfEditGuard reports whether path is one of this bot's own persona or
// profile files. A direct write to either still asks the owner even when the
// local machine switch is allow. It returns nil when the bot is not known.
func (k *Kernel) selfEditGuard() func(path string) bool {
	if k == nil || k.Bot == nil || k.Bot.Root == "" {
		return nil
	}
	dir := bot.Dir(k.Bot.Root, k.Bot.Name())
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	persona := filepath.Join(dir, "persona.md")
	profile := filepath.Join(dir, "config.json")
	return func(path string) bool {
		clean := filepath.Clean(path)
		return clean == persona || clean == profile
	}
}

func (k *Kernel) hasLink(name string) bool {
	return namedLink(k.snapshotLinks(), name)
}

func namedLink(links []link.Link, name string) bool {
	for _, l := range links {
		if l != nil && l.Name() == name {
			return true
		}
	}
	return false
}

func (k *Kernel) timer() (*link.Timer, error) {
	for _, l := range k.snapshotLinks() {
		if t, ok := l.(*link.Timer); ok && t != nil {
			return t, nil
		}
	}
	return nil, errors.New("timer: link is not mounted")
}

func schema(props map[string]any, required []string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

type sendMessageTool struct{ k *Kernel }

func (sendMessageTool) Name() string { return "send_message_to_user" }
func (sendMessageTool) Description() string {
	return "对用户说话的唯一可见通道：只有调用本工具用户才看得见；裸助手正文不可见（不是可选通知）。一次调用就是一个聊天气泡；用户不在聊天的唤醒里，它同时是通知。话要短、直白、好懂：不堆术语，不打官腔。importance：low、normal 或 high。images：粘贴 id 或工作区图片路径（PNG、JPEG、GIF、WebP），每条回复最多 4 张、合计 10 MiB、单边最长 2048 像素；给用户看本机图片只能走这里，不要发链接。audios：工作区语音路径或 voice id，最多 4 段合计 10 MiB；只挂语音，不做 TTS；纯音频时 text 可为空。"
}
func (sendMessageTool) Parameters() any {
	return schema(map[string]any{
		"text":       stringProp("what the owner should see"),
		"importance": stringProp("low, normal, or high; default normal"),
		"audios":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "workspace audio paths or voice ids to attach; generation is a separate tts tool"},
		"images": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "pasted image ids or workspace image paths to show in your reply",
		},
	}, []string{})
}

// Confirm's summary is the text, so the live chat can show the bubble when
// the call lands.
func (sendMessageTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &in)
	return false, strings.TrimSpace(in.Text), nil
}
func (t sendMessageTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Text       string   `json:"text"`
		Importance string   `json:"importance"`
		Images     []string `json:"images"`
		Audios     []string `json:"audios"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" && len(in.Audios) == 0 {
		return "", errors.New("send_message_to_user: empty message")
	}
	// Pictures are checked and stored before the note goes out, so a refused
	// picture does not leave a note that promises it.
	images, err := t.k.attachOutImages(ctx, in.Images, in.Audios)
	if err != nil {
		return "", err
	}
	// In owner chat the bubble is the delivery; the outbox and the desktop are
	// for wakes the owner is not watching.
	if turn := t.k.imageTurn(ctx); turn != nil && (turn.turnSource == inbox.NameTerminal || turn.turnSource == inbox.NameWeb) {
		if len(images) > 0 {
			return fmt.Sprintf("sent to the owner's chat with %d picture(s): %s", len(images), strings.Join(images, ", ")), nil
		}
		return "sent to the owner's chat", nil
	}
	if text == "" {
		text = "语音消息"
	}
	if err := deliverNote(ctx, t.k, text); err != nil {
		return "", err
	}
	if len(images) > 0 {
		return fmt.Sprintf("notified the owner; %d picture(s) will show in your reply bubble: %s", len(images), strings.Join(images, ", ")), nil
	}
	return "notified the owner", nil
}

func deliverNote(ctx context.Context, k *Kernel, text string) error {
	at := k.now()
	var name string
	if err := k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		name = b.Name()
		_, err := notify.Open(b.OutboxPath()).Append(text, at)
		return err
	}); err != nil {
		return err
	}
	desk := k.Desktop
	if desk == nil {
		desk = notify.Desktop
	}
	_ = desk(ctx, name, text)
	return nil
}

// pushFindings delivers a non-empty final reply from an untrusted wake.
// Owner chat on the terminal or web is not pushed. SILENT is not a finding.
// A send_message_to_user covers its input through that point, so its trailing reply
// is not sent again. A later incoming message can produce a new finding.
func (k *Kernel) pushFindings(ctx context.Context, produced []provider.Message) error {
	switch k.curTurn().turnSource {
	case inbox.NameTerminal, inbox.NameWeb:
		return nil
	}
	var replies []string
	covered := false
	for _, msg := range produced {
		// send_message_to_user covers the inputs it saw, not later arrivals. All ordering
		// comes from the normal conversation; no per-result receipt is involved.
		if msg.Role == provider.RoleTool && msg.Name == "send_message_to_user" && strings.Contains(msg.Text(), "notified the owner") {
			covered = true
			replies = nil
		}
		if msg.Role == provider.RoleUser && msg.EventID != "" {
			covered = false
		}
		if !covered && msg.Role == provider.RoleAssistant && len(msg.Calls()) == 0 && strings.TrimSpace(msg.Text()) != "" && !bot.SilentText(msg.Text()) {
			replies = append(replies, strings.TrimSpace(msg.Text()))
		}
	}
	text := strings.Join(replies, "\n\n")
	if text == "" || text == "SILENT" {
		return nil
	}
	if err := deliverNote(ctx, k, text); err != nil {
		return err
	}
	if k.OnFinding != nil {
		k.OnFinding(text)
	}
	return nil
}

func finalAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != provider.RoleAssistant {
			continue
		}
		if len(m.Calls()) > 0 {
			return ""
		}
		return strings.TrimSpace(m.Text())
	}
	return ""
}

// webSearchOn says the local web_search tool is offered: search is on and the
// bound provider has no hosted search of its own.
func (k *Kernel) webSearchOn() bool {
	root := ""
	if k.Bot != nil {
		root = k.Bot.Root
	}
	f, err := config.LoadWeb(root)
	return err == nil && f.SearchOn() && !provider.HasHostedSearch(k.boundProvider())
}

// bindingTools applies search exclusivity to a scoped tool set.
func (k *Kernel) bindingTools(list []tools.Tool) []tools.Tool {
	if k.webSearchOn() {
		return list
	}
	out := list[:0]
	for _, t := range list {
		if t.Name() != "web_search" {
			out = append(out, t)
		}
	}
	return out
}
