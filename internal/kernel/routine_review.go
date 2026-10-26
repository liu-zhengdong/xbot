package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/permissions"
	"xbot/internal/tools"
)

// routinePrecheck includes the existing command when an update leaves it in
// place, so re-enabling or rescheduling a check cannot bypass command review.
func (k *Kernel) routinePrecheck(name string, args json.RawMessage) (string, error) {
	var in struct {
		ID       string  `json:"id"`
		Precheck *string `json:"precheck"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.Precheck != nil {
		return *in.Precheck, nil
	}
	if name != "routine_update" || k == nil || k.Bot == nil {
		return "", nil
	}
	tm, err := k.timer()
	if err != nil {
		return "", err
	}
	r, err := tm.Load(in.ID)
	return r.Precheck, err
}

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

// CheckRoutinePrecheck applies the same shell review to an authenticated owner
// API write. This endpoint has no approval flow: commands that need a card must
// be set through the existing routine tool/owner approval flow instead.
func CheckRoutinePrecheck(ctx context.Context, b *bot.Bot, command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	k := &Kernel{Bot: b, PermissionsOn: func() bool { return permissions.On(b.Root) }}
	k.ensureTurn().turnTrusted = true
	need, _, err := k.reviewPrecheck(ctx, command, "update routine")
	if err != nil {
		return err
	}
	if need {
		return errors.New("precheck requires shell approval; use routine_update in chat")
	}
	return nil
}
