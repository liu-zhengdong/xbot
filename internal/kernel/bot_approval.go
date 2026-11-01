package kernel

import (
	"context"
	"errors"
)

// Strict deletion requires the original source call. The ordinary permissive
// path remains usable while the shared original-call bridge is connected.
func (k *Kernel) checkRuntimeBotDelete(_ context.Context, _ string) error {
	if k.permissive() {
		return nil
	}
	return errors.New("bot: source approval context unavailable")
}
