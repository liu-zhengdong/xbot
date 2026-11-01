package kernel

import (
	"context"
	"errors"
	"strings"
)

// checkRuntimeRoutinePrecheck requires the original source call for a command.
// Ordinary CRUD without an effective command does not need the approval bridge.
func (k *Kernel) checkRuntimeRoutinePrecheck(_ context.Context, command string, _ routineReviewRequest) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return errors.New("routine: source approval context unavailable")
}
