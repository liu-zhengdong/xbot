package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/peer"
)

type message struct {
	From  string `json:"from_bot"`
	To    string `json:"to_bot"`
	Text  string `json:"text"`
	Chain string `json:"chain"`
	Hops  int    `json:"hops"`
}

func Send(ctx context.Context, root, fromBot, addr, text, chain string, hops int, now time.Time) (queued bool, err error) {
	to, device, ok := strings.Cut(addr, "@")
	if !ok || !bot.ValidName(to) || !bot.ValidName(fromBot) {
		return false, errors.New("remote: expected bot@device")
	}
	cfg, err := Load(root)
	if err != nil {
		return false, err
	}
	d, ok := cfg.ByName(device)
	if !ok {
		return false, errors.New("remote: paired device required")
	}
	if d.XKey == "" {
		return false, fmt.Errorf("remote: %s has no encryption key; pair again with xbot remote invite/join", device)
	}
	sender, err := bot.Load(root, fromBot)
	if err != nil {
		return false, err
	}
	if !slices.Contains(sender.Config().Owners, d.Person) {
		return false, fmt.Errorf("remote: %s belongs to another person", device)
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > peer.MaxText || hops < 1 || hops > peer.MaxHops {
		return false, errors.New("remote: invalid text or hops")
	}
	text = strings.TrimSpace(bot.RedactText(text))
	if text == "" || len(text) > peer.MaxText {
		return false, errors.New("remote: invalid redacted text")
	}
	body, err := json.Marshal(message{fromBot, to, text, chain, hops})
	if err != nil {
		return false, err
	}
	priv, _, _, err := Identity(root)
	if err != nil {
		return false, err
	}
	env, err := newBoxEnvelope(priv, d, "box", body, now)
	if err != nil {
		return false, err
	}
	if err := (&peer.Bus{Root: root}).Allow("remote-out:"+fromBot+"->"+addr, now); err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err = (&gateway.Client{Base: cfg.GatewayURL(), Key: priv}).Send(ctx, env)
	if !retryable(err) {
		return false, err
	}
	err = enqueue(root, outboxItem{Env: env, FromBot: fromBot, To: addr, Snippet: string([]rune(text)[:min(80, len([]rune(text)))]), Created: now, Attempts: 1, Next: now.Add(5 * time.Second)})
	return err == nil, err
}
