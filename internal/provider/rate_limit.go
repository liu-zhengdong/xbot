package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultRateLimitWait = 3 * time.Minute

// ErrRateLimitWait is terminal for this model, but allows one configured fallback.
var ErrRateLimitWait = errors.New("provider: 限流等待过久")

type waitBudget struct{ used, max time.Duration }
type waitBudgetKey struct{}

// RateLimitSelector is the sole assembly seam for bounded waits and fallback.
// Each Select owns a fresh wake's budget and switches at most once.
type RateLimitSelector struct {
	Primary  ModelSelector
	Fallback ModelSelector
	MaxWait  time.Duration
}

func (s RateLimitSelector) Select(ctx context.Context, h Hint) (Provider, error) {
	p, err := s.Primary.Select(ctx, h)
	if err != nil {
		return nil, err
	}
	max := s.MaxWait
	if max <= 0 {
		max = DefaultRateLimitWait
	}
	return &limitedProvider{current: p, fallback: s.Fallback, hint: h, budget: waitBudget{max: max}}, nil
}

type limitedProvider struct {
	current  Provider
	fallback ModelSelector
	hint     Hint
	budget   waitBudget
}

func (p *limitedProvider) UnwrapProvider() Provider { return p.current }
func (p *limitedProvider) Chat(ctx context.Context, req Request, delta func(string)) (Message, error) {
	ctx = context.WithValue(ctx, waitBudgetKey{}, &p.budget)
	msg, err := p.current.Chat(ctx, req, delta)
	if !errors.Is(err, ErrRateLimitWait) || p.fallback == nil || ctx.Err() != nil {
		return msg, err
	}
	next, selectErr := p.fallback.Select(ctx, p.hint)
	p.fallback = nil
	if selectErr != nil {
		return Message{}, fmt.Errorf("%w; fallback unavailable: %v", err, selectErr)
	}
	// Keep the tool contract assembled for this wake: a hosted-capable backup
	// must not add a second search path beside an already offered local tool.
	for _, tool := range req.Tools {
		if tool.Name == "web_search" {
			next = WithoutHostedSearch(next)
			break
		}
	}
	p.current = next
	return p.current.Chat(ctx, req, delta)
}

// limitDelay only recognizes explicit quota/rate errors, never arbitrary input
// echoed in a JSON error. 401/403 are authentication errors, not quota walls.
func limitDelay(err error) (time.Duration, bool) {
	var visible *visibleResponseError
	if errors.As(err, &visible) {
		return 0, false
	}
	var s *statusError
	if !errors.As(err, &s) {
		return 0, false
	}
	if s.code == 401 || s.code == 403 {
		return 0, false
	}
	body := s.body
	var envelope struct {
		Error struct {
			Type, Code, Message string
			ResetsAt            int64   `json:"resets_at"`
			RetryAfter          float64 `json:"retry_after"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) == nil {
		body = envelope.Error.Type + " " + envelope.Error.Code + " " + envelope.Error.Message
	}
	text := strings.ToLower(body)
	limited := s.code == 429
	for _, marker := range []string{"usage_limit", "usage limit", "rate_limit", "insufficient_quota", "quota_exceeded", "额度耗尽", "等待额度"} {
		limited = limited || strings.Contains(text, marker)
	}
	if !limited {
		return 0, false
	}
	delay := s.retryAfter
	if delay == 0 && envelope.Error.RetryAfter > 0 {
		delay = time.Duration(envelope.Error.RetryAfter * float64(time.Second))
	}
	if delay == 0 && envelope.Error.ResetsAt > 0 {
		delay = time.Until(time.Unix(envelope.Error.ResetsAt, 0))
	}
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func retryAfterHeader(h http.Header, now time.Time) time.Duration {
	if raw := h.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
		if at, err := http.ParseTime(raw); err == nil && at.After(now) {
			return at.Sub(now)
		}
	}
	var longest time.Duration
	for _, key := range []string{"X-RateLimit-Reset", "X-RateLimit-Reset-Requests", "X-RateLimit-Reset-Tokens", "Anthropic-RateLimit-Requests-Reset", "Anthropic-RateLimit-Tokens-Reset"} {
		raw := h.Get(key)
		var d time.Duration
		if at, err := time.Parse(time.RFC3339, raw); err == nil {
			d = at.Sub(now)
		} else if duration, err := time.ParseDuration(raw); err == nil {
			d = duration
		} else if epoch, err := strconv.ParseInt(raw, 10, 64); err == nil {
			d = time.Unix(epoch, 0).Sub(now)
		}
		if d > longest {
			longest = d
		}
	}
	return longest
}
