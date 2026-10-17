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

func Send(ctx context.Context, root, fromBot, addr, text, chain string, hops int, now time.Time) error {
	to, device, ok := strings.Cut(addr, "@")
	if !ok || !bot.ValidName(to) || !bot.ValidName(fromBot) {
		return errors.New("remote: expected bot@device")
	}
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	d, ok := cfg.ByName(device)
	if !ok || cfg.Gateway == "" {
		return errors.New("remote: paired device and gateway required")
	}
	sender, err := bot.Load(root, fromBot)
	if err != nil {
		return err
	}
	if !slices.Contains(sender.Config().Owners, d.Person) {
		return fmt.Errorf("remote: %s belongs to another person", device)
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > peer.MaxText || hops < 1 || hops > peer.MaxHops {
		return errors.New("remote: invalid text or hops")
	}
	text = strings.TrimSpace(bot.RedactText(text))
	if text == "" || len(text) > peer.MaxText {
		return errors.New("remote: invalid redacted text")
	}
	body, err := json.Marshal(message{fromBot, to, text, chain, hops})
	if err != nil {
		return err
	}
	if len(body) > 64<<10 {
		return errors.New("remote: body exceeds 64 KiB")
	}
	priv, _, _, err := Identity(root)
	if err != nil {
		return err
	}
	if err := (&peer.Bus{Root: root}).Allow("remote-out:"+fromBot+"->"+addr, now); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return (&gateway.Client{Base: cfg.Gateway, Key: priv}).Send(ctx, gateway.NewEnvelope(priv, d.ID, "msg", body, now))
}
