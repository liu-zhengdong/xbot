package kernel

import (
	"xbot/internal/bot"
	"xbot/internal/provider"
)

// Why a tool call was closed without a real result. Interrupt, cancel, and
// model switch all go through DanglingResult so the pairing text cannot drift.
const (
	DangleInterrupt = "interrupt"
	DangleCancel    = "cancel"
	DangleSwitch    = "switch"
)

// DanglingResult is the tombstone for a tool call that will never return.
// Interrupt keeps the historical wording; cancel and model switch say which.
func DanglingResult(tc provider.ToolCall, reason string) provider.Message {
	return provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: tc.ID,
		Name:       tc.Function.Name,
		Content:    danglingText(reason),
	}
}

func danglingText(reason string) string {
	switch reason {
	case DangleCancel:
		return "cancelled before the tool returned"
	case DangleSwitch:
		return "model switched before the tool returned"
	default:
		return interrupted
	}
}

// closeDangling appends one tombstone per open call that can still be answered.
// skipRunning leaves a call on a live background alone: that call is in flight.
func (k *Kernel) closeDangling(reason string, skipRunning bool) error {
	if reason == "" {
		reason = DangleInterrupt
	}
	open, err := k.Bot.OpenTools()
	if err != nil || len(open) == 0 {
		return err
	}
	minSeq := open[0].CallSeq
	for _, t := range open[1:] {
		if t.CallSeq < minSeq {
			minSeq = t.CallSeq
		}
	}
	n, err := k.Bot.Count()
	if err != nil {
		return err
	}
	span, err := k.Bot.Slice(minSeq, n)
	if err != nil {
		return err
	}
	var live map[string]bool
	if skipRunning {
		live = map[string]bool{}
		k.bgMu.Lock()
		for id := range k.bgs {
			live[id] = true
		}
		k.bgMu.Unlock()
	}
	missing, branches, err := unresolvedCallsExcept(span, minSeq, live)
	if err != nil {
		return err
	}
	for _, tc := range missing {
		e := bot.Entry{
			Time:    k.now(),
			Branch:  branches[tc.ID],
			Message: DanglingResult(tc, reason),
		}
		if err := k.Bot.Append(e); err != nil {
			return err
		}
	}
	return nil
}

// danglingReason is switch when the bound model changed since the last wake
// and interrupt otherwise. An empty previous binding is a crash, not a switch.
func (k *Kernel) danglingReason() string {
	if k == nil || k.Bot == nil {
		return DangleInterrupt
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return DangleInterrupt
	}
	key := k.boundModelKey()
	if key != "" && st.BoundModel != "" && key != st.BoundModel {
		return DangleSwitch
	}
	return DangleInterrupt
}

func (k *Kernel) boundModelKey() string {
	if k == nil || k.Bot == nil {
		return ""
	}
	cfg := k.Bot.Config()
	if cfg.Provider == "" || cfg.Model == "" {
		return ""
	}
	return cfg.Provider + "/" + cfg.Model
}
