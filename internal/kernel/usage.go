package kernel

import (
	"context"
	"errors"
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
	// ratio is how many real prompt tokens one estimated token turned out to
	// be on the last call whose usage the API reported (see calibrate).
	ratio float64
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
	k.noteCallUsage(tr, req, msg, nil)
}

func (k *Kernel) noteCallUsage(tr *turn, req provider.Request, msg provider.Message, err error) {
	if k == nil || k.Bot == nil {
		return
	}
	u := observedUsage(req, msg)
	window := k.contextWindow()
	turnCompletion := 0
	branch := ""
	turnSeq := 0
	if tr != nil {
		tr.useMu.Lock()
		tr.useCompletion += u.CompletionTokens
		turnCompletion = tr.useCompletion
		tr.useMu.Unlock()
		tr.mu.Lock()
		branch = tr.branch
		turnSeq = tr.fromSeq
		tr.mu.Unlock()
	}
	k.tokens.mu.Lock()
	if msg.Usage.Reported && u.PromptTokens > 0 {
		k.tokens.ratio = calibrate(u.PromptTokens, compact.EstimateMessages(req.Messages)+estimateTools(req.ToolDefinitions()))
	}
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
	status := "completed"
	if errors.Is(err, context.Canceled) {
		status = "cancelled"
	} else if err != nil {
		status = "failed"
	}
	rec := bot.Trace{
		Status:            status,
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
		Turn:              turnSeq,
		CacheReported:     u.CacheReported,
	}
	if branch != "" {
		rec.Prompt, rec.Completion, rec.Cached = u.PromptTokens, u.CompletionTokens, u.CachedTokens
		rec.TurnCompletion, rec.Estimated, rec.Branch = turnCompletion, u.Estimated, branch
	}
	for _, p := range msg.Content {
		if p.Type == provider.PartHosted && p.Hosted != nil {
			rec.HostedCalls++
			rec.HostedTool = p.Hosted.Name
		}
	}
	_ = k.Bot.LogUsage(rec)
}

// noteCompactUsage records one summary call's usage, tagged so a cost count
// can include it. The conversation's own counts are not touched. turn is the
// transcript seq of the message that opened the turn the summary belongs to,
// or 0 for a summary a background job wrote on its own.
func (k *Kernel) noteCompactUsage(turn int, req provider.Request, msg provider.Message) {
	k.noteSideUsage(bot.TraceCallCompact, turn, req, msg)
}

// noteExtractUsage records one fact-extraction call's usage. It runs beside a
// summary and is part of the same compaction cost, so it carries its own tag.
func (k *Kernel) noteExtractUsage(turn int, req provider.Request, msg provider.Message) {
	k.noteSideUsage(bot.TraceCallExtract, turn, req, msg)
}

// noteSideUsage logs the usage of a call that is not part of the conversation,
// with the tag and turn that place it.
func (k *Kernel) noteSideUsage(call string, turn int, req provider.Request, msg provider.Message) {
	if k == nil || k.Bot == nil {
		return
	}
	u := observedUsage(req, msg)
	_ = k.Bot.LogUsage(bot.Trace{
		Time:          k.now(),
		Reason:        usageReason(u.Estimated),
		Prompt:        u.PromptTokens,
		Completion:    u.CompletionTokens,
		Cached:        u.CachedTokens,
		Estimated:     u.Estimated,
		Call:          call,
		Turn:          turn,
		CacheReported: u.CacheReported,
	})
}

// usageProvider notes the usage of every call through it.
type usageProvider struct {
	inner provider.Provider
	note  func(provider.Request, provider.Message)
}

func (p usageProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	msg, err := p.inner.Chat(ctx, req, onDelta)
	if err == nil {
		p.note(req, msg)
	}
	return msg, err
}

// compactor is k.Compactor with its model calls' usage recorded. A compactor
// that is not compact.LLM is returned as is: it has no calls to see. turn is
// the opening seq of the turn the summary belongs to, or 0 outside a turn.
func (k *Kernel) compactor(turn int) compact.Compactor {
	if l, ok := k.Compactor.(compact.LLM); ok && l.P != nil {
		l.P = usageProvider{inner: l.P, note: func(req provider.Request, msg provider.Message) {
			k.noteCompactUsage(turn, req, msg)
		}}
		return l
	}
	return k.Compactor
}

// extractor is k.Extractor with its model calls' usage recorded. An extractor
// that is not LLMExtractor is returned as is: it has no calls to see. turn is
// the opening seq of the turn the facts belong to, or 0 outside a turn.
func (k *Kernel) extractor(turn int) Extractor {
	if e, ok := k.Extractor.(LLMExtractor); ok && e.P != nil {
		e.Note = func(req provider.Request, msg provider.Message) {
			k.noteExtractUsage(turn, req, msg)
		}
		return e
	}
	return k.Extractor
}

// Ratio bounds. The estimate is never trusted to be larger than the real count
// (a picture's bytes overcount it, and shrinking on that would be unsafe), and
// a reading past twice the estimate is more likely a bad report than a
// tokenizer.
const (
	ratioMin = 1.0
	ratioMax = 2.0
)

// calibrate is the reported prompt count over the estimate of the same
// request, kept between ratioMin and ratioMax. Text that is mostly Chinese
// comes out near 1.3: the estimate counts a character as 0.75 of a token.
func calibrate(reported, estimated int) float64 {
	if estimated <= 0 {
		return ratioMin
	}
	r := float64(reported) / float64(estimated)
	if r < ratioMin {
		return ratioMin
	}
	if r > ratioMax {
		return ratioMax
	}
	return r
}

// scaled turns an estimate into the real count the last reported call implies,
// so the lines are compared with what the model will actually count.
func (k *Kernel) scaled(n int) int {
	if k == nil {
		return n
	}
	k.tokens.mu.Lock()
	r := k.tokens.ratio
	k.tokens.mu.Unlock()
	if r <= ratioMin {
		return n
	}
	return int(float64(n) * r)
}

// overflowed records that the provider rejected a request the estimate said
// would fit: the real count is larger than the ratio claims, so it is raised by
// a quarter before the request is shrunk and sent again.
func (k *Kernel) overflowed() {
	k.tokens.mu.Lock()
	defer k.tokens.mu.Unlock()
	r := k.tokens.ratio
	if r < ratioMin {
		r = ratioMin
	}
	r *= 1.25
	if r > ratioMax {
		r = ratioMax
	}
	k.tokens.ratio = r
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

// contextNote fires when prompt tokens cross 50, 75, or 90 percent of the hard
// ceiling: the line past which the request would be rejected, which is what the
// model has to steer by (fewer large reads, shorter outputs). The soft target
// is the kernel's own business; the background compacts at it. The raw window
// is not used either, because it leaves no room for the reply or the tools.
// The same threshold is not repeated until usage falls below it and crosses it again.
func (k *Kernel) contextNote(prev []Reminder) string {
	if k == nil {
		return ""
	}
	// The ceiling looks up the model window under k.tokens.mu, so it is read
	// before that lock is taken here.
	ceiling := k.ceiling()
	k.tokens.mu.Lock()
	defer k.tokens.mu.Unlock()
	if !k.tokens.seen || k.tokens.prompt <= 0 {
		return ""
	}
	if !k.tokens.hwSet {
		k.tokens.hw = contextLevelOf(prev)
		k.tokens.hwSet = true
	}
	level := usageLevel(k.tokens.prompt, ceiling)
	if level > k.tokens.hw {
		k.tokens.hw = level
		return contextLine(level, k.tokens.snap(), ceiling)
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

// contextLine names the limit the percentage is against, and the model window
// separately, so a reader can tell how much room is left before the request
// would be rejected and how much the provider allows.
func contextLine(level int, snap TokenUse, budget int) string {
	est := ""
	approx := ""
	if snap.Estimated {
		est = " estimated (the API did not report usage)"
		approx = "~"
	}
	return fmt.Sprintf("<context_usage level=%q>\ncontext %d%%%s of the %d limit (prompt %s%d, window %d, cached %d, turn completion %d, session completion %d)\n</context_usage>",
		strconv.Itoa(level), level, est, budget, approx, snap.Prompt, snap.Window, snap.Cached, snap.TurnCompletion, snap.SessionCompletion)
}
