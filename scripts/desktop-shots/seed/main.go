// Command seed builds a throwaway XBot data root so the desktop app opens on a
// realistic window for screenshots. It uses no network and no real key.
package main

import (
	"fmt"
	"os"
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
	case "chat", "empty", "rich":
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

// at returns today's local wall-clock time, stored the way the kernel stores it.
func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.Local).UTC()
}
