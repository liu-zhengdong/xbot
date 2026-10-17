package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"xbot/internal/code"
	"xbot/internal/config"
	"xbot/internal/eval"
	"xbot/internal/exec"
	"xbot/internal/tools"
)

func cmdRunner(args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"addr": true, "token": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:18771", "loopback listen address")
	token := fs.String("token", "", "shared bearer token")
	yes := fs.Bool("yes", false, "approve shell and file writes inside subagents")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot runner --addr 127.0.0.1:18771 --token TOKEN")
	}
	tok := *token
	if tok == "" {
		var b [24]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		tok = hex.EncodeToString(b[:])
		fmt.Printf("token: %s\n", tok)
	}
	fmt.Fprintf(os.Stderr, "runner listening on %s\n", *addr)
	loc := exec.NewLocal(exec.Limits{})
	loc.Approve = func(context.Context, string, string) (bool, error) { return *yes, nil }
	loc.ToolSet = func(string, string) []tools.Tool {
		return []tools.Tool{tools.NewShell(), &tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}}
	}
	return exec.ListenCode(*addr, tok, loc, code.NewService())
}

func cmdEval(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"suite": true, "out": true, "runs": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	suite := fs.String("suite", "", "one embedded suite or a directory; empty runs both certification suites")
	out := fs.String("out", "", "directory for the JSON and Markdown report")
	runs := fs.Int("runs", 3, "times to run each case; the case passes by majority")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: xbot eval <provider/model> [--suite NAME|DIR] [--runs 3] [--out DIR]")
	}
	if *runs < 1 {
		return errors.New("eval: --runs must be positive")
	}
	opt := eval.Options{
		Root: root, Model: fs.Arg(0), Suite: *suite, OutDir: *out, Runs: *runs,
		Progress: func(c eval.CaseResult) {
			for i, run := range c.Runs {
				mark := "fail"
				if run.Pass {
					mark = "pass"
				}
				fmt.Printf("%s\t%s\t%d\t%s\n", c.Name, c.Level, i+1, mark)
			}
			mark := "fail"
			if c.Pass {
				mark = "pass"
			}
			fmt.Printf("%s\t%s\t%s\n", c.Name, c.Level, mark)
		},
	}
	if *suite != "" {
		if info, err := os.Stat(*suite); err == nil && info.IsDir() {
			opt.CasesDir = *suite
		}
	}
	rep, err := eval.Run(opt)
	if rep.JSON != "" {
		fmt.Printf("report %s\n", rep.JSON)
	}
	if err != nil {
		return err
	}
	if rep.Certified {
		fmt.Printf("certified %s\n", fs.Arg(0))
	} else {
		fmt.Printf("passed %s\n", fs.Arg(0))
	}
	return nil
}

func cmdProvider(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot provider add|list|remove")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("provider list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		all := fs.Bool("all", false, "include models that have not passed the proactivity suite")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: xbot provider list [--all]")
		}
		f, err := config.Load(root)
		if err != nil {
			return err
		}
		shown, err := config.VisibleModels(root, *all)
		if err != nil {
			return err
		}
		if len(f.Providers) == 0 {
			fmt.Println("no providers")
			return nil
		}
		byProvider := map[string][]config.ModelInfo{}
		for _, m := range shown {
			byProvider[m.Provider] = append(byProvider[m.Provider], m)
		}
		for _, p := range f.Providers {
			fmt.Printf("%s\t%s\tkey=%s\n", p.Name, p.BaseURL, p.APIKeyEnv)
			models := byProvider[p.Name]
			if len(models) == 0 {
				if *all {
					fmt.Println("  (no models)")
				} else {
					fmt.Println("  (no certified models)")
				}
				continue
			}
			for _, m := range models {
				mark := "certified"
				if !m.Certified {
					mark = "uncertified"
				}
				fmt.Printf("  %s\tcontext=%d\t%s\n", m.Model, m.Context, mark)
			}
		}
		return nil
	case "remove":
		ordered, err := reorderFlags(args[1:], map[string]bool{"model": true})
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("provider remove", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		model := fs.String("model", "", "model id to remove")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: xbot provider remove <name>")
		}
		if err := config.Remove(root, fs.Arg(0), *model); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", fs.Arg(0))
		return nil
	case "add":
		ordered, err := reorderFlags(args[1:], map[string]bool{
			"base-url": true, "api-key-env": true, "model": true, "context": true,
			"todo": true, "todo-steps": true, "thinking": true,
		})
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("provider add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		base := fs.String("base-url", "", "OpenAI-compatible base URL")
		keyEnv := fs.String("api-key-env", "", "environment variable that holds the API key")
		model := fs.String("model", "", "model id")
		contextLen := fs.Int("context", 0, "context length in tokens")
		todoFlag := fs.String("todo", "", "todo reminders: on, off, or empty to keep the current setting")
		todoSteps := fs.Int("todo-steps", 0, "tool steps without a todo update before a reminder; 0 keeps the default")
		thinking := fs.String("thinking", "", "thinking field: off, omit, or empty to keep the current setting")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: xbot provider add <name> --base-url URL --api-key-env VAR --model NAME --context N [--todo on|off] [--todo-steps N] [--thinking off|omit]")
		}
		spec := config.ModelSpec{Name: *model, Context: *contextLen, TodoSteps: *todoSteps, Thinking: strings.ToLower(strings.TrimSpace(*thinking))}
		if prev, _, err := config.Lookup(root, fs.Arg(0), *model); err == nil {
			for _, m := range prev.Models {
				if m.Name == *model {
					spec.Todo = m.Todo
					if spec.TodoSteps == 0 {
						spec.TodoSteps = m.TodoSteps
					}
				}
			}
		}
		switch strings.ToLower(strings.TrimSpace(*todoFlag)) {
		case "":
		case "on", "true":
			on := true
			spec.Todo = &on
		case "off", "false":
			off := false
			spec.Todo = &off
		default:
			return errors.New("provider add: --todo must be on or off")
		}
		if err := config.Add(root, config.Provider{
			Name: fs.Arg(0), BaseURL: *base, APIKeyEnv: *keyEnv,
		}, spec); err != nil {
			return err
		}
		fmt.Printf("saved provider %s\n", fs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
}
