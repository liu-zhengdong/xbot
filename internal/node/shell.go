package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/machine"
	"xbot/internal/notify"
	"xbot/internal/tools"
)

const (
	// PendingTTL is how long an ask command stays runnable.
	PendingTTL = 10 * time.Minute
	// PendingCap is the most unexpired commands this machine will queue.
	PendingCap = 32
)

// pendingID is np_ plus 16 hex chars. Anything else is not a file name we open.
var pendingID = regexp.MustCompile(`^np_[0-9a-f]{16}$`)

// Pending is one command waiting for the owner of this machine.
type Pending struct {
	ID      string    `json:"id"`
	Command string    `json:"command"`
	Caller  string    `json:"caller"`
	Time    time.Time `json:"time"`
}

// RunShell executes command on this machine when the local switch is allow.
// approved is ignored. A remote caller cannot mark the command already confirmed.
// ask stores the command and returns an error that names xbot node approve.
// caller is recorded for the owner; an empty caller is stored as "local".
func RunShell(root, command, caller string, approved bool) (string, error) {
	_ = approved
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("node: empty command")
	}
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return "", err
	}
	switch mode {
	case machine.Off:
		return "", fmt.Errorf("machine local is off")
	case machine.Ask:
		id, err := enqueue(root, caller, command)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("machine local is ask; xbot node approve %s", id)
	case machine.Allow:
	default:
		return "", fmt.Errorf("machine: bad mode %q", mode)
	}
	return runCommand(command)
}

// ListPending returns unexpired commands, oldest first, and deletes expired files.
func ListPending(root string) ([]Pending, error) {
	if err := prune(root, time.Now()); err != nil {
		return nil, err
	}
	return readPending(root)
}

// LookupPending returns one unexpired command.
func LookupPending(root, id string) (Pending, error) {
	if err := prune(root, time.Now()); err != nil {
		return Pending{}, err
	}
	return readOne(root, id)
}

// Dismiss removes a pending command without running it.
func Dismiss(root, id string) error {
	path, err := pendingPath(root, id)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("node: no pending %s", strings.TrimSpace(id))
	}
	return err
}

// Approve runs a command that ask left on disk. The owner types this on the node.
// off still refuses, and a missing or expired id does nothing.
func Approve(root, id string) (string, error) {
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return "", err
	}
	if mode == machine.Off {
		return "", fmt.Errorf("machine local is off")
	}
	if err := prune(root, time.Now()); err != nil {
		return "", err
	}
	item, err := readOne(root, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(item.Command) == "" {
		return "", fmt.Errorf("node: empty command")
	}
	path, err := pendingPath(root, id)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return runCommand(item.Command)
}

// LocalCall is the /v1/call handler for this machine. It does not trust Request.Approved.
// The status capability is added by the cmd wiring, where the data root is known.
func LocalCall(root string, info Info) func(Request) (string, error) {
	return func(req Request) (string, error) {
		switch strings.TrimSpace(req.Cap) {
		case "info":
			return InfoText(info), nil
		case "shell":
			return RunShell(root, req.Command, req.Caller, req.Approved)
		default:
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
	}
}

func runCommand(command string) (string, error) {
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return "", err
	}
	return tools.NewShell().Run(context.Background(), raw)
}

func enqueue(root, caller, command string) (string, error) {
	now := time.Now().UTC()
	if err := prune(root, now); err != nil {
		return "", err
	}
	have, err := readPending(root)
	if err != nil {
		return "", err
	}
	if len(have) >= PendingCap {
		return "", errors.New("node: too many pending")
	}
	caller = strings.TrimSpace(caller)
	if caller == "" {
		caller = "local"
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	item := Pending{ID: "np_" + hex.EncodeToString(b[:]), Command: command, Caller: caller, Time: now}
	raw, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "node-pending", item.ID+".json")
	if err := fsutil.WriteAtomic(path, append(raw, '\n'), 0600); err != nil {
		return "", err
	}
	notifyPending(root, item)
	return item.ID, nil
}

func notifyPending(root string, item Pending) {
	cfgs, err := bot.List(root)
	if err != nil || len(cfgs) == 0 {
		return
	}
	text := fmt.Sprintf("node approve %s\ncaller: %s\n%s", item.ID, item.Caller, item.Command)
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			continue
		}
		_, _ = notify.Open(b.OutboxPath()).Append(text, item.Time)
	}
}

func prune(root string, now time.Time) error {
	items, err := readPending(root)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !expired(item, now) {
			continue
		}
		path, err := pendingPath(root, item.ID)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func expired(item Pending, now time.Time) bool {
	if item.Time.IsZero() {
		return true
	}
	return now.Sub(item.Time.UTC()) > PendingTTL
}

func readPending(root string) ([]Pending, error) {
	dir := filepath.Join(root, "node-pending")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Pending
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(ent.Name(), ".json")
		item, err := readOne(root, id)
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time.Equal(out[j].Time) {
			return out[i].ID < out[j].ID
		}
		return out[i].Time.Before(out[j].Time)
	})
	return out, nil
}

func readOne(root, id string) (Pending, error) {
	path, err := pendingPath(root, id)
	if err != nil {
		return Pending{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Pending{}, fmt.Errorf("node: no pending %s", strings.TrimSpace(id))
		}
		return Pending{}, err
	}
	var item Pending
	if err := json.Unmarshal(raw, &item); err != nil {
		return Pending{}, err
	}
	if item.ID == "" {
		item.ID = id
	}
	return item, nil
}

func pendingPath(root, id string) (string, error) {
	id = strings.TrimSpace(id)
	if !pendingID.MatchString(id) {
		return "", fmt.Errorf("node: bad id")
	}
	return filepath.Join(root, "node-pending", id+".json"), nil
}
