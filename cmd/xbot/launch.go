package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultCommand is set by desktop packaging (-ldflags "-X main.defaultCommand=app")
// so that double-clicking XBot.app or XBot.exe opens the window. The plain CLI
// build leaves it empty and still prints usage when run without arguments.
var defaultCommand string

// launchArgs drops the -psn_* argument older macOS versions pass to apps
// started from Finder.
func launchArgs(argv []string) []string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if !strings.HasPrefix(a, "-psn_") {
			out = append(out, a)
		}
	}
	return out
}

// loadEnvFile sets KEY=VALUE lines from path for variables that are not set
// yet. Values may be quoted; unquoted values end at " #". Nothing is expanded
// or executed. A desktop app started
// from Finder or Explorer has no shell environment, so this is where it finds
// provider keys. A missing file is not an error. Values are never printed.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) > 0 && (val[0] == '"' || val[0] == '\'') {
			end := strings.IndexByte(val[1:], val[0])
			if end < 0 {
				continue
			}
			val = val[1 : end+1]
		} else if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// logAppError appends a startup failure of the double-clicked app to
// <root>/app.log: a GUI build has no console, so stderr goes nowhere. Errors
// here come from locking, listening and the window, not from secrets.
func logAppError(root string, err error) {
	f, ferr := os.OpenFile(filepath.Join(root, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if ferr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s xbot app: %v\n", time.Now().Format(time.RFC3339), err)
}
