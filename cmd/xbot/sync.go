package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"xbot/internal/fsutil"
	"xbot/internal/memory"
	"xbot/internal/person"
)

type syncBundle struct {
	People []person.LogEntry `json:"people"`
	Memory []memory.SyncLine `json:"memory"`
}

func cmdSync(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot sync export --out FILE | xbot sync merge --file FILE")
	}
	switch args[0] {
	case "export":
		return cmdSyncExport(root, args[1:])
	case "merge":
		return cmdSyncMerge(root, args[1:])
	default:
		return errors.New("usage: xbot sync export --out FILE | xbot sync merge --file FILE")
	}
}

func cmdSyncExport(root string, args []string) error {
	fs := flag.NewFlagSet("sync export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "bundle file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" || fs.NArg() != 0 {
		return errors.New("usage: xbot sync export --out FILE")
	}
	people, err := person.ExportLog(root)
	if err != nil {
		return err
	}
	mem, err := memory.ExportSync(root)
	if err != nil {
		return err
	}
	if people == nil {
		people = []person.LogEntry{}
	}
	if mem == nil {
		mem = []memory.SyncLine{}
	}
	raw, err := fsutil.Marshal(syncBundle{People: people, Memory: mem})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(*out, raw, 0600)
}

func cmdSyncMerge(root string, args []string) error {
	fs := flag.NewFlagSet("sync merge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "bundle file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" || fs.NArg() != 0 {
		return errors.New("usage: xbot sync merge --file FILE")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var b syncBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if err := person.MergeLog(root, b.People); err != nil {
		return err
	}
	return memory.MergeSync(root, b.Memory)
}
