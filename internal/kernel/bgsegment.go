package kernel

import (
	"fmt"
	"time"
)

const (
	// defaultSegmentCap is how many segments one background task may run, the
	// first one included, before it stops and reports.
	defaultSegmentCap = 4
	// defaultBackgroundMaxTime caps the wall-clock time all segments of one
	// background task may spend together.
	defaultBackgroundMaxTime = 20 * time.Minute
	// fallbackMaxSteps mirrors the agent loop's own default, used only to size
	// the cumulative step cap when the kernel was built without one.
	fallbackMaxSteps = 8
)

// continueNote starts another segment of a background task whose previous
// segment ended at its step cap. The owner does not see it; it is a user-role
// message on the background branch.
const continueNote = "[继续] 上一段用完了步数上限，接着做，不要重复已完成的工作。"

// segmentCap is the segment limit in effect.
func (k *Kernel) segmentCap() int {
	if k.MaxSegments > 0 {
		return k.MaxSegments
	}
	return defaultSegmentCap
}

// backgroundMaxTime is the cumulative wall-clock limit in effect. A negative
// value turns it off.
func (k *Kernel) backgroundMaxTime() time.Duration {
	if k.BackgroundMaxTime != 0 {
		return k.BackgroundMaxTime
	}
	return defaultBackgroundMaxTime
}

// totalStepCap is the cumulative step limit across all segments of one task.
func (k *Kernel) totalStepCap() int {
	steps := k.MaxSteps
	if steps <= 0 {
		steps = fallbackMaxSteps
	}
	return k.segmentCap() * steps
}

// continueSegmentLocked decides whether the background turn t, whose last
// segment ended at its step cap, may run another segment. It increments
// t.segments when it may. When it may not, it records why on the turn so
// finishBackground can report it, and returns ("", false). The caller holds t.mu.
// queued says the background still has unread controls: they earn a segment of
// their own, because an owner control that was accepted must not be dropped,
// but the caps below still bound that segment.
func (k *Kernel) continueSegmentLocked(t *turn, queued bool) (bool, string) {
	if !t.capStopped && !queued {
		// The segment ended with an answer (or an error), not at the cap: the
		// task is done as far as the model is concerned.
		return false, ""
	}
	if t.segments >= k.segmentCap() {
		return false, fmt.Sprintf("已用 %d 段，达到段数上限", t.segments)
	}
	if t.stepsUsed >= k.totalStepCap() {
		return false, fmt.Sprintf("已用 %d 步，达到累计步数上限 %d", t.stepsUsed, k.totalStepCap())
	}
	if max := k.backgroundMaxTime(); max > 0 && k.now().Sub(t.started) >= max {
		return false, fmt.Sprintf("已跑 %s，达到累计墙钟上限 %s", k.now().Sub(t.started).Round(time.Second), max)
	}
	t.segments++
	return true, ""
}

// markSegmentStop records why the background cannot continue, so the completion
// event can say where it got stuck.
func (k *Kernel) markSegmentStop(t *turn, reason string) {
	if reason == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopReason = reason
}
