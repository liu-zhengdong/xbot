package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/tools"
)

func (k *Kernel) reviewPrecheck(ctx context.Context, command, summary string) (bool, string, error) {
	if k == nil {
		k = &Kernel{}
	}
	if len(command) > tools.MaxPrecheckBytes {
		return false, "", errors.New("precheck exceeds 16 KiB")
	}
	// Prechecks run in the process cwd, not the foreground/branch shell's
	// session directory. Pass that actual cwd to the existing path guards.
	cwd, err := os.Getwd()
	if err != nil {
		return false, "", err
	}
	args, err := json.Marshal(map[string]string{"command": command, "cwd": cwd})
	if err != nil {
		return false, "", err
	}
	return k.reviewCall(ctx, &tools.Shell{}, args, fmt.Sprintf("%s; precheck sh -c %q", summary, command))
}

// CheckRoutinePrecheck applies production's always-allow shell review to an
// authenticated owner API write, matching the routine tool path (ADR 0019).
func CheckRoutinePrecheck(ctx context.Context, b *bot.Bot, command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	k := &Kernel{Bot: b, Permissive: true}
	k.ensureTurn().turnTrusted = true
	need, _, err := k.reviewPrecheck(ctx, command, "update routine")
	if err != nil {
		return err
	}
	if need {
		return errors.New("precheck requires shell approval; use the original source routine CLI in chat")
	}
	return nil
}
