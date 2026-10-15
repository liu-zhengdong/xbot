// Package work is the local workspace for a bot's shell and file tools.
package work

import (
	"fmt"
	"os"
	"path/filepath"
)

// BotDir is the default working directory for one bot: ~/.xbot/work/<bot>/.
func BotDir(root, name string) string {
	return filepath.Join(root, "work", name)
}

// SharedDir is the directory every bot on this root can see: ~/.xbot/work/shared/.
func SharedDir(root string) string {
	return filepath.Join(root, "work", "shared")
}

// Open creates the bot directory and the shared directory.
func Open(root, name string) (botDir, shared string, err error) {
	if !botOK(name) {
		return "", "", fmt.Errorf("work: invalid bot name %q", name)
	}
	botDir = BotDir(root, name)
	shared = SharedDir(root)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(shared, 0700); err != nil {
		return "", "", err
	}
	return botDir, shared, nil
}

func botOK(name string) bool {
	return name != "" && name != "." && name != ".." && name != "shared" && filepath.Base(name) == name
}
