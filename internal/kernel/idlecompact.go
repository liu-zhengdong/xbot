package kernel

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func compactDisabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "off", "0", "no", "关":
		return true
	}
	return false
}

func idleCompactPolicy() (minimum int, after time.Duration) {
	minimum, after = 16000, 50*time.Minute
	raw := strings.TrimSpace(os.Getenv("XBOT_IDLE_COMPACT_MIN_TOKENS"))
	if compactDisabled(raw) {
		return 0, 0
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		minimum = n
	}
	raw = strings.TrimSpace(os.Getenv("XBOT_IDLE_COMPACT_AFTER"))
	if compactDisabled(raw) {
		return 0, 0
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		after = d
	}
	return
}

// MaybeIdleCompact is called by the existing serve pass while it holds the
// bot's handle lock. It only admits a job; the worker never holds that lock.
// A durable running/ready job deduplicates passes, including after a restart.
func (k *Kernel) MaybeIdleCompact(now time.Time) {
	minimum, after := idleCompactPolicy()
	if minimum == 0 || k == nil || k.Bot == nil || k.Compactor == nil {
		return
	}
	if err := k.CheckBindingIdle(); err != nil {
		return
	}
	n, err := k.Bot.Count()
	if err != nil || n == 0 {
		return
	}
	tail, err := k.Bot.Slice(n, n)
	if err != nil || len(tail) != 1 || tail[0].Time.IsZero() || now.Sub(tail[0].Time) < after {
		return
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return
	}
	k.startBackground(migrate(st), 0, st.MemSnap, minimum, true)
}
