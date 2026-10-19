package kernel

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/provider"
)

const KindContext = "context_usage"

// tokenState tracks model-reported usage for this kernel. Session totals last
// as long as the kernel does. A new process starts them at zero; trace.jsonl
// keeps the history.
type tokenState struct {
	mu             sync.Mutex
	prompt         int
	completion     int
	cached         int
	turnCompletion int
	sessPrompt     int
	sessComp       int
	sessCached     int
	estimated      bool
	window         int
	windowKey      string
	windowChecked  bool
	hw             int
	hwSet          bool
	seen           bool
}

// TokenUse is the compact usage line on BotStatus. Prompt, completion, and
// cached are the latest model call. Session fields accumulate for this kernel.
// Estimated is true when the latest prompt count was not reported by the API.
type TokenUse struct {
	Prompt            int  `json:"prompt,omitempty"`
	Completion        int  `json:"completion,omitempty"`
	Cached            int  `json:"cached,omitempty"`
	TurnCompletion    int  `json:"turn_completion,omitempty"`
	SessionPrompt     int  `json:"session_prompt,omitempty"`
	SessionCompletion int  `json:"session_completion,omitempty"`
	SessionCached     int  `json:"session_cached,omitempty"`
	Window            int  `json:"window,omitempty"`
	Estimated         bool `json:"estimated,omitempty"`
}

func (s *tokenState) snap() TokenUse {
	return TokenUse{
		Prompt:            s.prompt,
		Completion:        s.completion,
		Cached:            s.cached,
		TurnCompletion:    s.turnCompletion,
		SessionPrompt:     s.sessPrompt,
		SessionCompletion: s.sessComp,
		SessionCached:     s.sessCached,
		Window:            s.window,
		Estimated:         s.estimated,
	}
}

// noteUsage records one model call. Real API numbers are kept as they are.
// When the response has no usage object, prompt and completion are a rough
// byte estimate and Estimated is set. The latest counts, which drive the
// context-usage reminder and BotStatus, belong to the foreground: a call from
// a background branch adds to the session totals and is logged with its own
// counts and branch, but does not replace them.
func (k *Kernel) noteUsage(tr *turn, req provider.Request, msg provider.Message) {
	if k == nil || k.Bot == nil {
		return
	}
	u := observedUsage(req, msg)
	window := k.contextWindow()
	turnCompletion := 0
	branch := ""
	if tr != nil {
		tr.useMu.Lock()
		tr.useCompletion += u.CompletionTokens
		turnCompletion = tr.useCompletion
		tr.useMu.Unlock()
		tr.mu.Lock()
		branch = tr.branch
		tr.mu.Unlock()
	}
	k.tokens.mu.Lock()
	if branch == "" {
		k.tokens.prompt = u.PromptTokens
		k.tokens.completion = u.CompletionTokens
		k.tokens.cached = u.CachedTokens
		k.tokens.estimated = u.Estimated
		k.tokens.turnCompletion = turnCompletion
	}
	k.tokens.sessPrompt += u.PromptTokens
	k.tokens.sessComp += u.CompletionTokens
	k.tokens.sessCached += u.CachedTokens
	k.tokens.window = window
	k.tokens.seen = true
	snap := k.tokens.snap()
	k.tokens.mu.Unlock()
	rec := bot.Trace{
		Time:              k.now(),
		Reason:            usageReason(u.Estimated),
		Prompt:            snap.Prompt,
		Completion:        snap.Completion,
		Cached:            snap.Cached,
		TurnCompletion:    snap.TurnCompletion,
		SessionPrompt:     snap.SessionPrompt,
		SessionCompletion: snap.SessionCompletion,
		SessionCached:     snap.SessionCached,
		Window:            snap.Window,
		Estimated:         snap.Estimated,
	}
	if branch != "" {
		rec.Prompt, rec.Completion, rec.Cached = u.PromptTokens, u.CompletionTokens, u.CachedTokens
		rec.TurnCompletion, rec.Estimated, rec.Branch = turnCompletion, u.Estimated, branch
	}
	_ = k.Bot.LogUsage(rec)
}

func usageReason(estimated bool) string {
	if estimated {
		return "estimated"
	}
	return "reported"
}

func observedUsage(req provider.Request, msg provider.Message) provider.TokenUsage {
	if msg.Usage.Reported {
		u := msg.Usage
		u.Estimated = false
		return u
	}
	return provider.TokenUsage{
		PromptTokens:     compact.EstimateMessages(req.Messages),
		CompletionTokens: compact.EstimateMessage(msg),
		Estimated:        true,
	}
}

// contextWindow is the bound model's context length from providers.json.
// Zero means the registry has no length, so no percentage is announced.
func (k *Kernel) contextWindow() int {
	if k == nil || k.Bot == nil {
		return 0
	}
	key := k.boundModelKey()
	k.tokens.mu.Lock()
	if k.tokens.windowChecked && k.tokens.windowKey == key {
		n := k.tokens.window
		k.tokens.mu.Unlock()
		return n
	}
	k.tokens.mu.Unlock()
	n := lookupWindow(k, key)
	k.tokens.mu.Lock()
	k.tokens.window = n
	k.tokens.windowKey = key
	k.tokens.windowChecked = true
	k.tokens.mu.Unlock()
	return n
}

func lookupWindow(k *Kernel, key string) int {
	if k == nil || k.Bot == nil || k.Bot.Root == "" || key == "" {
		return 0
	}
	cfg := k.Bot.Config()
	_, spec, err := config.Lookup(k.Bot.Root, cfg.Provider, cfg.Model)
	if err != nil || spec.Context <= 0 {
		return 0
	}
	return spec.Context
}

func (k *Kernel) tokenSnap() (TokenUse, bool) {
	if k == nil {
		return TokenUse{}, false
	}
	k.tokens.mu.Lock()
	defer k.tokens.mu.Unlock()
	if !k.tokens.seen {
		return TokenUse{}, false
	}
	return k.tokens.snap(), true
}

func (k *Kernel) contextTrigger() func(time.Time, []Reminder) string {
	return func(_ time.Time, prev []Reminder) string {
		return k.contextNote(prev)
	}
}

// contextNote fires when prompt tokens cross 50, 75, or 90 percent of the
// working set, which is the size the session is held near, not the model
// window. A window is usually far larger: against it a session sitting at its
// working set would read as a small fraction and no threshold would ever fire.
// The same threshold is not repeated until usage falls below it and crosses it again.
func (k *Kernel) contextNote(prev []Reminder) string {
	if k == nil {
		return ""
	}
	k.tokens.mu.Lock()
	defer k.tokens.mu.Unlock()
	if !k.tokens.seen || k.tokens.prompt <= 0 {
		return ""
	}
	if !k.tokens.hwSet {
		k.tokens.hw = contextLevelOf(prev)
		k.tokens.hwSet = true
	}
	level := usageLevel(k.tokens.prompt, k.compactAt())
	if level > k.tokens.hw {
		k.tokens.hw = level
		return contextLine(level, k.tokens.snap(), k.compactAt())
	}
	if level < k.tokens.hw {
		k.tokens.hw = level
	}
	return ""
}

func usageLevel(prompt, budget int) int {
	if budget <= 0 || prompt <= 0 {
		return 0
	}
	pct := prompt * 100 / budget
	switch {
	case pct >= 90:
		return 90
	case pct >= 75:
		return 75
	case pct >= 50:
		return 50
	default:
		return 0
	}
}

func contextLevelOf(prev []Reminder) int {
	last := 0
	for _, p := range prev {
		if p.Kind != KindContext {
			continue
		}
		if n := parseLevel(p.Text); n > 0 {
			last = n
		}
	}
	return last
}

func parseLevel(s string) int {
	const key = `level="`
	i := strings.Index(s, key)
	if i < 0 {
		return 0
	}
	rest := s[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		return 0
	}
	return n
}

// contextLine names the working set the percentage is against, and the model
// window separately, so a reader can tell how much room is left in the session
// and how much the provider allows.
func contextLine(level int, snap TokenUse, budget int) string {
	est := ""
	approx := ""
	if snap.Estimated {
		est = " estimated (the API did not report usage)"
		approx = "~"
	}
	return fmt.Sprintf("<context_usage level=%q>\ncontext %d%%%s of the %d working set (prompt %s%d, window %d, cached %d, turn completion %d, session completion %d)\n</context_usage>",
		strconv.Itoa(level), level, est, budget, approx, snap.Prompt, snap.Window, snap.Cached, snap.TurnCompletion, snap.SessionCompletion)
}
