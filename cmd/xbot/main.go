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
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/person"
	"xbot/internal/provider"
)

const usageText = `XBot is a personal bot. One bot has one continuous conversation.

Usage:
  xbot [--root DIR] new <name> [--persona TEXT]
  xbot [--root DIR] list
  xbot [--root DIR] chat <name> [--yes] [--max-steps N]

Environment:
  XBOT_API_KEY    API key (required for chat)
  XBOT_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com/v1)
  XBOT_MODEL      Model name (default deepseek-chat)
`

var (
	errHelp  = errors.New("help")
	errUsage = errors.New("usage")
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errHelp) || errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usageText)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "xbot:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	root, rest, err := splitGlobal(argv)
	if err != nil {
		return err
	}
	if root == "" {
		root, err = defaultRoot()
		if err != nil {
			return err
		}
	}
	if len(rest) == 0 {
		return errUsage
	}
	switch rest[0] {
	case "new":
		return cmdNew(root, rest[1:])
	case "list":
		return cmdList(root)
	case "chat":
		return cmdChat(root, rest[1:])
	default:
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

func defaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".xbot"), nil
}

func splitGlobal(argv []string) (string, []string, error) {
	var root string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--help" || a == "-h" || a == "help":
			return "", nil, errHelp
		case a == "--root" || a == "-root":
			if i+1 >= len(argv) {
				return "", nil, errors.New("missing value for --root")
			}
			i++
			root = argv[i]
		case strings.HasPrefix(a, "--root="):
			root = strings.TrimPrefix(a, "--root=")
		default:
			return root, argv[i:], nil
		}
	}
	return root, nil, errUsage
}

type newOpts struct {
	Name    string
	Persona string
}

func parseNew(args []string) (newOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"persona": true})
	if err != nil {
		return newOpts{}, err
	}
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	persona := fs.String("persona", bot.DefaultPersona, "system persona")
	if err := fs.Parse(ordered); err != nil {
		return newOpts{}, err
	}
	if fs.NArg() != 1 {
		return newOpts{}, errors.New("usage: xbot new <name>")
	}
	return newOpts{Name: fs.Arg(0), Persona: *persona}, nil
}

func cmdNew(root string, args []string) error {
	opt, err := parseNew(args)
	if err != nil {
		return err
	}
	if _, err := bot.Create(root, opt.Name, opt.Persona, []string{link.NameTerminal, link.NameTools}); err != nil {
		return err
	}
	fmt.Printf("created bot %s\n", opt.Name)
	return nil
}

func cmdList(root string) error {
	bots, err := bot.List(root)
	if err != nil {
		return err
	}
	if len(bots) == 0 {
		fmt.Println("no bots")
		return nil
	}
	for _, cfg := range bots {
		fmt.Printf("%s\t%s\n", cfg.Name, strings.Join(cfg.Links, ","))
	}
	return nil
}

type chatOpts struct {
	Name     string
	Yes      bool
	MaxSteps int
}

func parseChat(args []string) (chatOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"max-steps": true})
	if err != nil {
		return chatOpts{}, err
	}
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "approve shell commands and file writes without prompting")
	steps := fs.Int("max-steps", 8, "maximum model calls per wake")
	if err := fs.Parse(ordered); err != nil {
		return chatOpts{}, err
	}
	if fs.NArg() != 1 {
		return chatOpts{}, errors.New("usage: xbot chat <name>")
	}
	return chatOpts{Name: fs.Arg(0), Yes: *yes, MaxSteps: *steps}, nil
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
	cfg := config.FromEnv(os.Getenv)
	if cfg.APIKey == "" {
		return errors.New("XBOT_API_KEY is empty")
	}
	b, err := bot.Load(root, opt.Name)
	if err != nil {
		return err
	}
	if !b.Grants(link.NameTerminal) {
		return fmt.Errorf("bot %q does not grant the terminal link", opt.Name)
	}
	links, err := mount(b.Config().Links)
	if err != nil {
		return err
	}
	ext, display, err := link.Account()
	if err != nil {
		return err
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		return err
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return err
	}
	client := &provider.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pr := &printer{yes: opt.Yes, reader: bufio.NewReader(os.Stdin)}
	k := &kernel.Kernel{
		Bot:       b,
		Provider:  client,
		Links:     links,
		Inbox:     box,
		Memory:    memory.NopStore{},
		Compactor: compact.LLM{P: client},
		Approve:   pr.Approve,
		MaxSteps:  opt.MaxSteps,
		Budget:    kernel.BudgetFor(b.Config()),
		OnDelta:   pr.Delta,
		OnTool:    pr.Tool,
		OnResult:  pr.Result,
	}
	k.StartEmitters(ctx)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if ev, ok := box.Head(); ok {
			err := k.Handle(ctx, ev)
			pr.Break()
			if err != nil {
				fmt.Fprintln(os.Stderr, "xbot:", err)
				if errors.Is(err, context.Canceled) {
					return nil
				}
			}
			if err := box.Ack(ev.ID); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "%s › ", b.Name())
		line, err := readLine(ctx, pr.reader)
		text := strings.TrimRight(line, "\r\n")
		queued := false
		if strings.TrimSpace(text) != "" && !isQuit(text) {
			ev := link.TerminalEvent(who.ID, text, time.Now())
			if postErr := box.Post(ev); postErr != nil {
				return postErr
			}
			queued = true
		}
		if isQuit(text) {
			return nil
		}
		if err != nil {
			if queued {
				continue
			}
			return nil
		}
	}
}

func mount(names []string) ([]link.Link, error) {
	var out []link.Link
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		switch name {
		case link.NameTerminal:
			out = append(out, link.Terminal{})
		case link.NameTools:
			out = append(out, link.NewToolsLink())
		case link.NameTimer:
			return nil, fmt.Errorf("link %q is not available in M1 (timer interface only)", name)
		default:
			return nil, fmt.Errorf("unknown link %q", name)
		}
	}
	return out, nil
}

func isQuit(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "exit", "quit", ":q":
		return true
	default:
		return false
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

type printer struct {
	yes    bool
	reader *bufio.Reader
	wrote  bool
	nl     bool
}

func (p *printer) Delta(s string) {
	fmt.Fprint(os.Stdout, s)
	p.wrote = true
	p.nl = strings.HasSuffix(s, "\n")
}

func (p *printer) Break() {
	if p.wrote && !p.nl {
		fmt.Fprintln(os.Stdout)
	}
	p.wrote = false
}

func (p *printer) Tool(name, summary string) {
	p.Break()
	fmt.Fprintf(os.Stderr, "[%s] %s\n", name, summary)
}

func (p *printer) Result(_, output string) {
	shown, _ := clip.Bytes(output, 400)
	fmt.Fprintln(os.Stderr, shown)
}

func (p *printer) Approve(ctx context.Context, _, _ string) (bool, error) {
	if p.yes {
		return true, nil
	}
	fmt.Fprint(os.Stderr, "Allow? [y/n] ")
	line, err := readLine(ctx, p.reader)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
