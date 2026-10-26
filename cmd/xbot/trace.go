package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"xbot/internal/bot"
	"xbot/internal/vault"
)

func cmdTrace(root string, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: xbot trace paths|list|replay [bot]")
	}
	fs := flag.NewFlagSet("trace "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var sinceText, untilText, kind string
	var from, to int
	flags := map[string]bool{}
	switch args[0] {
	case "paths":
	case "list":
		fs.StringVar(&sinceText, "since", "", "inclusive RFC3339 lower bound")
		fs.StringVar(&untilText, "until", "", "inclusive RFC3339 upper bound")
		fs.StringVar(&kind, "kind", "", "trace kind")
		flags = map[string]bool{"since": true, "until": true, "kind": true}
	case "replay":
		fs.IntVar(&from, "from", 0, "inclusive transcript sequence")
		fs.IntVar(&to, "to", 0, "inclusive transcript sequence")
		flags = map[string]bool{"from": true, "to": true}
	default:
		return fmt.Errorf("unknown trace command %q", args[0])
	}
	ordered, err := reorderFlags(args[1:], flags)
	if err != nil {
		return err
	}
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("trace: expected at most one bot")
	}
	var since, until time.Time
	for _, bound := range []struct {
		text string
		dst  *time.Time
		name string
	}{{sinceText, &since, "since"}, {untilText, &until, "until"}} {
		if bound.text != "" {
			*bound.dst, err = time.Parse(time.RFC3339, bound.text)
			if err != nil {
				return fmt.Errorf("trace: --%s must be RFC3339", bound.name)
			}
		}
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return errors.New("trace: --since must not be after --until")
	}
	name := fs.Arg(0)
	if name == "" {
		name, err = bot.Primary(root)
		if err != nil {
			return err
		}
	}
	b, err := bot.Load(root, name)
	if err != nil {
		return err
	}
	if args[0] == "paths" {
		paths, err := b.Paths()
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(paths)
	}
	store, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		return err
	}
	if args[0] == "replay" {
		return bot.ExportTo(out, b, bot.TranscriptExport{Tools: true, From: from, To: to, Redact: store.Redact})
	}
	return listTrace(out, b.TracePath(), since, until, kind, store.Redact)
}

// Read a bounded snapshot of the existing telemetry log. A concurrent append's
// incomplete final line is deferred to the next invocation, just like ReadJSONL.
// No transcript parsing belongs here: replay always uses ExportTo.
func listTrace(out io.Writer, path string, since, until time.Time, kind string, redact func(string) string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(io.LimitReader(f, info.Size()))
	sc.Buffer(make([]byte, 4096), 1<<20)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	})
	for line := 1; sc.Scan(); line++ {
		raw := sc.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var event bot.Trace
		if err := json.Unmarshal(raw, &event); err != nil {
			// Do not echo malformed file contents, which could contain credentials.
			return fmt.Errorf("trace: invalid JSON record at line %d", line)
		}
		if kind != "" && event.Kind != kind {
			continue
		}
		if !since.IsZero() && event.Time.Before(since) {
			continue
		}
		if !until.IsZero() && event.Time.After(until) {
			continue
		}
		if _, err := fmt.Fprintln(out, redact(string(raw))); err != nil {
			return err
		}
	}
	return sc.Err()
}
