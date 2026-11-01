package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clock"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/proc"
	"xbot/internal/provider"
)

type chatOpts struct {
	Name        string
	Yes         bool
	RunnerURL   string
	RunnerToken string
}

func parseChat(args []string) (chatOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"runner": true, "runner-token": true})
	if err != nil {
		return chatOpts{}, err
	}
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "approve shell commands and file writes without prompting")
	runnerURL := fs.String("runner", "", "remote executor base URL")
	runnerToken := fs.String("runner-token", "", "remote executor token")
	if err := fs.Parse(ordered); err != nil {
		return chatOpts{}, err
	}
	if fs.NArg() > 1 {
		return chatOpts{}, errors.New("usage: xbot chat [name]")
	}
	name := ""
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	return chatOpts{Name: name, Yes: *yes, RunnerURL: *runnerURL, RunnerToken: *runnerToken}, nil
}

// reorderFlags moves flags in front of positional args so the name can sit
// before or after them. valueFlags names flags that consume the next argument.
func reorderFlags(args []string, valueFlags map[string]bool) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if cut, _, ok := strings.Cut(name, "="); ok {
			name = cut
		}
		flags = append(flags, a)
		if !strings.Contains(a, "=") && valueFlags[name] {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("missing value for --%s", name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...), nil
}

func cmdChat(root string, args []string) error {
	opt, err := parseChat(args)
	if err != nil {
		return err
	}
	if opt.Name == "" {
		if err := ensureSeed(root); err != nil {
			return err
		}
		opt.Name, err = bot.Primary(root)
		if err != nil {
			return err
		}
	}
	b, err := bot.Load(root, opt.Name)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	lines := make(chan proc.Line)
	pr := &printer{yes: opt.Yes, lines: lines}
	manager := mcp.NewManager(ctx, mcpOptions(root))
	defer manager.Close()
	resources, err := kernel.NewBotResources(ctx, b, manager, clock.Now)
	if err != nil {
		return err
	}
	defer resources.Close()
	resources.Rename = func(source *kernel.Kernel, change bot.ProfileChange) (bot.ProfileChangeResult, error) {
		return source.MoveResources(change)
	}
	resources.Delete = func(ctx context.Context, name string) (bool, error) {
		if name == resources.Name() {
			return resources.DeleteRuntime(ctx, name)
		}
		return bot.DeleteWithResult(root, name)
	}
	k, who, err := openKernel(root, b, 0, pr.Approve, pr, opt.Yes, runnerSpec{opt.RunnerURL, opt.RunnerToken}, resources)
	if err != nil {
		return err
	}
	defer k.Close()
	defer k.CloseOperations()
	k.BindOperationsContext(ctx)
	k.StartEmitters(ctx)
	cfg, err := chatConfig(k, who.ID, lines, pr)
	if err != nil {
		return err
	}
	go feedLines(bufio.NewReader(os.Stdin), lines)
	return proc.Run(ctx, cfg)
}

// withChatIO keeps each short file operation on the original resource's current
// Bot/Inbox. Terminal output and interactive callbacks run after it returns.
func withChatIO(k *kernel.Kernel, fn func(*bot.Bot, *inbox.Inbox) error) error {
	if k.Resources != nil {
		return k.Resources.WithCurrentIO(fn)
	}
	return fn(k.Bot, k.Inbox)
}

// chatConfig assembles the terminal callbacks used by the ordinary proc loop.
func chatConfig(k *kernel.Kernel, who string, lines <-chan proc.Line, pr *printer) (proc.Config, error) {
	var seen int
	err := withChatIO(k, func(current *bot.Bot, _ *inbox.Inbox) error { var err error; seen, err = current.Count(); return err })
	if err != nil {
		return proc.Config{}, err
	}
	return proc.Config{
		K:     k,
		Lines: lines,
		Accept: func(text string) (link.Event, error) {
			text = strings.TrimRight(text, "\r\n")
			if isQuit(text) {
				return link.Event{}, proc.ErrStop
			}
			if strings.TrimSpace(text) == "" {
				return link.Event{}, nil
			}
			ev := link.TerminalEvent(who, strings.TrimSpace(text), time.Now())
			if err := withChatIO(k, func(_ *bot.Bot, box *inbox.Inbox) error { return box.Post(ev) }); err != nil {
				return link.Event{}, err
			}
			return ev, nil
		},
		Idle: func() {
			if err := showNotes(k); err != nil {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
			name := k.Bot.Name()
			if k.Resources != nil {
				name = k.Resources.Name()
			}
			fmt.Fprintf(os.Stderr, "%s › ", name)
		},
		CatchUp: func() bool {
			return showTranscriptSince(k, &seen)
		},
		After: func(_ link.Event, err error) {
			pr.Break()
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
			_ = withChatIO(k, func(current *bot.Bot, _ *inbox.Inbox) error {
				n, err := current.Count()
				if err == nil {
					seen = n
				}
				return err
			})
			if err := showNotes(k); err != nil {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
		},
	}, nil
}

// showTranscriptSince prints user and assistant lines appended by another
// process, such as a web turn that ran while this chat sat at the prompt.
func showTranscriptSince(k *kernel.Kernel, seen *int) bool {
	if k == nil || k.Bot == nil || seen == nil {
		return false
	}
	var entries []bot.Entry
	err := withChatIO(k, func(current *bot.Bot, _ *inbox.Inbox) error {
		var err error
		entries, err = current.Entries()
		return err
	})
	if err != nil || len(entries) <= *seen {
		return false
	}
	fresh := entries[*seen:]
	*seen = len(entries)
	printed := false
	for _, e := range fresh {
		text := strings.TrimSpace(e.Message.Text())
		if text == "" || strings.Contains(text, "<environment_") || bot.SilentText(text) {
			continue
		}
		switch e.Message.Role {
		case provider.RoleUser, provider.RoleAssistant:
			if len(e.Message.Calls()) > 0 {
				continue
			}
			fmt.Fprintln(os.Stdout, text)
			printed = true
		}
	}
	return printed
}

func isQuit(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "exit", "quit", ":q":
		return true
	default:
		return false
	}
}

func feedLines(r *bufio.Reader, lines chan<- proc.Line) {
	defer close(lines)
	for {
		line, err := r.ReadString('\n')
		lines <- proc.Line{Text: line, Err: err}
		if err != nil || isQuit(line) {
			return
		}
	}
}

func readLine(ctx context.Context, r *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-ch:
		return res.line, res.err
	}
}
