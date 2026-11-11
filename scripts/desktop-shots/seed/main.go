// Command seed builds a throwaway XBot data root so the desktop app opens on a
// realistic window for screenshots. It uses no network and no real key.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/person"
	"xbot/internal/provider"
)

const key = "ci-screenshot-not-a-key"

// links mirrors the grants cmd/xbot gives the first bot.
var links = []string{
	link.NameTerminal, link.NameTools, link.NameTimer,
	link.NameMemory, link.NamePersona, link.NameContext,
	link.NameAgents, link.NameSkills,
	link.NameWebhook, link.NameWatch,
	link.NameWeb, link.NamePeers,
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: seed <data-root> [chat|empty|rich]")
		os.Exit(2)
	}
	mode := "chat"
	if len(os.Args) == 3 {
		mode = os.Args[2]
	}
	if err := seed(os.Args[1], mode); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
	fmt.Printf("seeded %s (%s)\n", os.Args[1], mode)
}

func seed(root, mode string) error {
	switch mode {
	case "chat", "empty", "rich", "relay":
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := config.SaveEnvKey(root, "XBOT_API_KEY", key); err != nil {
		return err
	}
	ext, display, err := link.Account()
	if err != nil {
		return err
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		return err
	}
	b, created, err := bot.EnsurePrimary(root, bot.Seed{Persona: bot.DefaultPersona, Links: links, Owner: who.ID})
	if err != nil {
		return err
	}
	defer bot.CloseStore(b)
	if created {
		if err := b.WriteRules(kernel.DefaultRules()); err != nil {
			return err
		}
	}
	if mode == "empty" {
		return nil
	}
	now := time.Now()
	ask := bot.Entry{Time: at(now, 10, 1), Message: provider.Message{Role: provider.RoleUser, Content: "你好"}}
	if err := b.Append(ask); err != nil {
		return err
	}
	reply := bot.Entry{Time: at(now, 10, 2), Message: provider.Message{Role: provider.RoleAssistant, Content: "你好！有什么可以帮你的？"}}
	if err := b.Append(reply); err != nil {
		return err
	}
	if mode == "relay" {
		return seedRelay(b, now)
	}
	if mode != "rich" {
		return nil
	}
	for _, extra := range []struct{ name, persona string }{
		{"日记", "记下每天的小事，语气温和。"},
		{"开发", "帮你理清代码和工程问题，回答简短直接。"},
		{"健康", "关注睡眠、饮食和运动，给容易做到的建议。"},
	} {
		nb, err := bot.Create(root, extra.name, extra.persona, links)
		if err != nil {
			return err
		}
		if err := nb.SetOwners([]string{who.ID}); err != nil {
			return err
		}
	}
	diary, err := bot.Load(root, "日记")
	if err != nil {
		return err
	}
	defer bot.CloseStore(diary)
	note := bot.Entry{Time: at(now, 9, 40), Message: provider.Message{Role: provider.RoleAssistant, Content: "今天天气不错，记得出去走一走。"}}
	return diary.Append(note)
}

// seedRelay adds bot-to-bot traffic: a run with writer on a paired device
// (sent, received, sent, received, one bounce), a reply to the owner, then a
// single message from dev on this computer.
func seedRelay(b *bot.Bot, now time.Time) error {
	ask := func(id, to, text string) provider.Message {
		args, _ := json.Marshal(map[string]string{"bot": to, "text": text})
		return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: string(args)}}}}
	}
	from := func(source, text string, t time.Time) provider.Message {
		sender := strings.TrimPrefix(source, "bot:")
		return provider.Message{Role: provider.RoleUser, Content: link.Wrap(link.NewEvent(source, link.TypeMessage, text, sender, t))}
	}
	for _, e := range []bot.Entry{
		{Time: at(now, 10, 37), Message: provider.Message{Role: provider.RoleUser, Content: "帮我问问 writer，周末那篇游记写到哪了。"}},
		{Time: at(now, 10, 38), Message: provider.Message{Role: provider.RoleAssistant, Content: "好，我去问 MacBook 上的 writer。"}},
		{Time: at(now, 10, 39), Message: ask("c1", "writer@MacBook", "writer，周末那篇游记写到哪了？主人想知道进度。")},
		{Time: at(now, 10, 39), Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "sent"}},
		{Time: at(now, 10, 41), Message: from("bot:writer@MacBook", "游记写到第二天下午了，还差晚上那段。另外请把你记的周六行程发我一份，我照着补细节。", at(now, 10, 41))},
		{Time: at(now, 10, 42), Message: ask("c2", "writer@MacBook", "行程我先问问主人。")},
		{Time: at(now, 10, 42), Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c2", Content: "sent"}},
		{Time: at(now, 10, 43), Message: from("bot:writer@MacBook", "好，等你消息。", at(now, 10, 43))},
		{Time: at(now, 10, 44), Message: ask("c3", "writer@MacBook", "主人还没回，稍后再说。")},
		{Time: at(now, 10, 44), Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c3", Content: "sent"}},
		{Time: at(now, 10, 45), Message: from("remote", "message to writer@MacBook was not delivered because the device is offline: 主人还没回，稍后再说。", at(now, 10, 45))},
		{Time: at(now, 10, 46), Message: provider.Message{Role: provider.RoleAssistant, Content: "writer 说游记写到第二天下午，还差晚上那段。它想要周六的行程来补细节，要我把日记里的行程发给它吗？"}},
		{Time: at(now, 11, 5), Message: from("bot:dev", "测试都过了，等你看一眼。", at(now, 11, 5))},
	} {
		if err := b.Append(e); err != nil {
			return err
		}
	}
	return nil
}

// at returns today's local wall-clock time, stored the way the kernel stores it.
func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.Local).UTC()
}
