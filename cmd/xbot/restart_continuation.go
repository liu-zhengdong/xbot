package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/exec"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

const restartContinuationFile = ".restart-continuation.json"

type restartContinuation struct {
	ID   string   `json:"id"`
	Hint string   `json:"hint,omitempty"`
	Bots []string `json:"bots"`
}

func restartContinuationPath(root string) string {
	return filepath.Join(root, restartContinuationFile)
}

func requestBotRestart(root, hint string, restart func(string)) error {
	if err := saveRestartContinuation(root, hint); err != nil {
		return err
	}
	restart("Bot requested restart")
	return nil
}

// saveRestartContinuation snapshots every bot with a held foreground handle or
// a live/queued subagent, then persists one shared continuation before restart.
func saveRestartContinuation(root, hint string) error {
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	var active []string
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			return err
		}
		working := false
		lock, ok, err := b.TryLockHandle()
		if err != nil {
			return err
		}
		if ok {
			_ = lock.Release()
		} else {
			working = true
		}
		entries, err := os.ReadDir(b.AgentsDir())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			meta, err := exec.ReadMeta(filepath.Join(b.AgentsDir(), entry.Name()))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			if meta.Status == "running" || meta.Status == "queued" {
				working = true
				break
			}
		}
		if working {
			active = append(active, cfg.Name)
		}
	}
	sort.Strings(active)
	if len(active) == 0 {
		return errors.New("restart refused: no working Bots to continue")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	state := restartContinuation{ID: hex.EncodeToString(idBytes), Hint: strings.TrimSpace(hint), Bots: active}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(restartContinuationPath(root), raw, 0600)
}

// deliverRestartContinuation posts deterministic per-Bot inbox IDs. A crash
// during fan-out is safe: startup retries and each inbox deduplicates repeated IDs.
func deliverRestartContinuation(root string) error {
	path := restartContinuationPath(root)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state restartContinuation
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.ID == "" {
		return errors.New("restart continuation has no id")
	}
	for _, name := range state.Bots {
		b, err := bot.Load(root, name)
		if errors.Is(err, bot.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return err
		}
		content := "XBot 已重启。请从被打断的工作继续；如有续跑提示，以用户提示为准。"
		if state.Hint != "" {
			content += "\n用户的续跑提示：" + state.Hint
		}
		sum := sha256.Sum256([]byte(state.ID + "\x00" + name))
		ev := link.NewEvent("restart", link.TypeMessage, content, "xbot", time.Now())
		ev.ID = "restart_" + hex.EncodeToString(sum[:16])
		if err := box.Post(ev); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
