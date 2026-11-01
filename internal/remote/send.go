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

// SendResult reports the gateway and durable queue facts of one send attempt.
type SendResult struct {
	EnvelopeID    string
	Gateway       string
	GatewayStatus int
	Queued        bool
	Stage         string
}

func Send(ctx context.Context, root, fromBot, addr, text, chain string, hops int, now time.Time) (queued bool, err error) {
	result, err := SendWithResult(ctx, root, fromBot, addr, text, chain, hops, now)
	return result.Queued, err
}

func SendWithResult(ctx context.Context, root, fromBot, addr, text, chain string, hops int, now time.Time) (result SendResult, err error) {
	result.Gateway = "not_attempted"
	result.Stage = "validation"
	to, device, ok := strings.Cut(addr, "@")
	if !ok || !bot.ValidName(to) || !bot.ValidName(fromBot) {
		return result, errors.New("remote: expected bot@device")
	}
	cfg, err := Load(root)
	if err != nil {
		return result, err
	}
	d, ok := cfg.ByName(device)
	if !ok {
		return result, errors.New("remote: paired device required")
	}
	if !d.UnpairedAt.IsZero() {
		return result, fmt.Errorf("remote: device %s was unpaired by the other computer", device)
	}
	// 「Bot 互发消息」 off: no envelope, no queue; the outbox is left as it is.
	if d.NoBotMessaging {
		return result, fmt.Errorf("remote: bot messages to %s are turned off in 设备", device)
	}
	if d.XKey == "" {
		return result, fmt.Errorf("remote: %s has no encryption key; pair again with xbot remote invite/join", device)
	}
	sender, err := bot.Load(root, fromBot)
	if err != nil {
		return result, err
	}
	if !slices.Contains(sender.Config().Owners, d.Person) {
		return result, fmt.Errorf("remote: %s belongs to another person", device)
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > peer.MaxText || hops < 1 || hops > peer.MaxHops {
		return result, errors.New("remote: invalid text or hops")
	}
	text = strings.TrimSpace(bot.RedactText(text))
	if text == "" || len(text) > peer.MaxText {
		return result, errors.New("remote: invalid redacted text")
	}
	body, err := json.Marshal(message{fromBot, to, text, chain, hops})
	if err != nil {
		return result, err
	}
	result.Stage = "identity"
	priv, _, _, err := Identity(root)
	if err != nil {
		return result, err
	}
	result.Stage = "envelope"
	env, err := newBoxEnvelope(priv, d, "box", body, now)
	if err != nil {
		return result, err
	}
	result.EnvelopeID = env.ID
	result.Stage = "rate"
	if err := (&peer.Bus{Root: root}).Allow("remote-out:"+fromBot+"->"+addr, now); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result.Stage = "gateway"
	err = (&gateway.Client{Base: cfg.GatewayURL(), Key: priv}).Send(ctx, env)
	if err == nil {
		result.Gateway = "accepted"
		result.Stage = "complete"
		return result, nil
	}
	result.Gateway = "unknown"
	var status *gateway.HTTPError
	if errors.As(err, &status) {
		result.Gateway = "http_error"
		result.GatewayStatus = status.StatusCode
	}
	if !retryable(err) {
		return result, err
	}
	result.Stage = "queue"
	err = enqueue(root, outboxItem{Env: env, FromBot: fromBot, To: addr, Snippet: string([]rune(text)[:min(80, len([]rune(text)))]), Created: now, Attempts: 1, Next: now.Add(5 * time.Second)})
	result.Queued = err == nil
	if result.Queued {
		result.Stage = "complete"
	}
	return result, err
}
