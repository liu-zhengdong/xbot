package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type commandDriver struct{}

func (commandDriver) Screenshot(ctx context.Context, path string) error {
	return run(ctx, "import", "-window", "root", path)
}

func (commandDriver) Click(ctx context.Context, x, y int) error {
	return run(ctx, "xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "1")
}

func (commandDriver) Type(ctx context.Context, text string) error {
	return run(ctx, "xdotool", "type", "--", text)
}

func (commandDriver) Scroll(ctx context.Context, dy int) error {
	button := "4"
	n := dy
	if dy < 0 {
		button = "5"
		n = -dy
	}
	return run(ctx, "xdotool", "click", "--repeat", strconv.Itoa(n), button)
}

func (commandDriver) Key(ctx context.Context, key string) error {
	return run(ctx, "xdotool", "key", "--", key)
}

func (commandDriver) Idle(ctx context.Context) (time.Duration, bool) {
	out, err := exec.CommandContext(ctx, "xprintidle").Output()
	if err != nil {
		return 0, false
	}
	ms, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || ms < 0 {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if tail == "" {
			return fmt.Errorf("desktop: %s: %w", name, err)
		}
		if len(tail) > 300 {
			tail = tail[len(tail)-300:]
		}
		return fmt.Errorf("desktop: %s: %s", name, tail)
	}
	return nil
}
