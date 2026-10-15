// Package desktop drives the local pointer, keyboard, and screen.
// It yields while the owner is active, and the machine switch can refuse it.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xbot/internal/machine"
)

const idleYield = 5 * time.Second

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_+-]{1,64}$`)

// Action is one desktop call.
type Action struct {
	Name string
	X, Y int
	Text string
	Dy   int
	Key  string
}

// Driver performs the action on this machine. Tests substitute a fake.
type Driver interface {
	Screenshot(ctx context.Context, path string) error
	Click(ctx context.Context, x, y int) error
	Type(ctx context.Context, text string) error
	Scroll(ctx context.Context, dy int) error
	Key(ctx context.Context, key string) error
	Idle(ctx context.Context) (time.Duration, bool)
}

// Perform runs one action. approved is true after the owner confirmed, including a CLI the owner typed.
// off always refuses. ask refuses when approved is false.
func Perform(ctx context.Context, root string, d Driver, action Action, approved bool) (string, error) {
	if d == nil {
		d = commandDriver{}
	}
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return "", err
	}
	if mode == machine.Off {
		return "", errors.New("machine local is off")
	}
	if mode == machine.Ask && !approved {
		return "", errors.New("machine local is ask")
	}
	mutating := action.Name != "screenshot"
	if mutating {
		if idle, ok := d.Idle(ctx); ok && idle < idleYield {
			return "", errors.New("desktop: owner is active")
		}
	}
	switch action.Name {
	case "screenshot":
		dir := filepath.Join(root, "desktop")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		path := filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000")+".png")
		if err := d.Screenshot(ctx, path); err != nil {
			return "", err
		}
		return path, nil
	case "click":
		if action.X < 0 || action.Y < 0 || action.X > 10000 || action.Y > 10000 {
			return "", errors.New("desktop: click is outside 0..10000")
		}
		if err := d.Click(ctx, action.X, action.Y); err != nil {
			return "", err
		}
		return fmt.Sprintf("clicked %d %d", action.X, action.Y), nil
	case "type":
		text := action.Text
		if strings.TrimSpace(text) == "" {
			return "", errors.New("desktop: empty text")
		}
		if len(text) > 2000 {
			return "", errors.New("desktop: text is too long")
		}
		if err := d.Type(ctx, text); err != nil {
			return "", err
		}
		return "typed " + fmt.Sprint(len([]rune(text))) + " chars", nil
	case "scroll":
		if action.Dy == 0 || action.Dy > 20 || action.Dy < -20 {
			return "", errors.New("desktop: scroll dy must be -20..-1 or 1..20")
		}
		if err := d.Scroll(ctx, action.Dy); err != nil {
			return "", err
		}
		return fmt.Sprintf("scrolled %d", action.Dy), nil
	case "key":
		key := strings.TrimSpace(action.Key)
		if !keyPattern.MatchString(key) {
			return "", errors.New("desktop: bad key")
		}
		if err := d.Key(ctx, key); err != nil {
			return "", err
		}
		return "key " + key, nil
	default:
		return "", fmt.Errorf("desktop: unknown action %q", action.Name)
	}
}

// NeedsConfirm reports whether the owner must approve this action under mode.
func NeedsConfirm(mode string, action Action) bool {
	if mode == machine.Ask {
		return true
	}
	return action.Name != "screenshot"
}
