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
	"xbot/internal/notify"
	"xbot/internal/person"
	"xbot/internal/provider"
	"xbot/internal/serve"
)

const usageText = `XBot is a personal bot. One bot has one continuous conversation.

Usage:
  xbot [--root DIR] new <name> [--persona TEXT] [--provider NAME] [--model NAME]
  xbot [--root DIR] list
  xbot [--root DIR] chat <name> [--yes] [--max-steps N]
  xbot [--root DIR] bind <name> --provider NAME --model NAME
  xbot [--root DIR] bind <name> --clear
  xbot [--root DIR] serve [--interval 15s]
  xbot [--root DIR] provider add <name> --base-url URL --api-key-env VAR --model NAME --context N
  xbot [--root DIR] provider list
  xbot [--root DIR] provider remove <name> [--model NAME]

Environment:
  XBOT_API_KEY    API key when a bot has no provider binding
  XBOT_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com/v1)
  XBOT_MODEL      Model name (default deepseek-chat)

A named provider stores the environment variable name, never the key itself.
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
	case "bind":
		return cmdBind(root, rest[1:])
	case "serve":
		return cmdServe(root, rest[1:])
	case "provider":
		return cmdProvider(root, rest[1:])
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

var defaultLinks = []string{
	link.NameTerminal, link.NameTools, link.NameTimer,
	link.NameMemory, link.NamePersona, link.NameContext,
}

type newOpts struct {
	Name     string
	Persona  string
	Provider string
	Model    string
}

func parseNew(args []string) (newOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"persona": true, "provider": true, "model": true})
	if err != nil {
		return newOpts{}, err
	}
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	persona := fs.String("persona", bot.DefaultPersona, "system persona")
	prov := fs.String("provider", "", "named provider")
	model := fs.String("model", "", "model id")
	if err := fs.Parse(ordered); err != nil {
		return newOpts{}, err
	}
	if fs.NArg() != 1 {
		return newOpts{}, errors.New("usage: xbot new <name>")
	}
	return newOpts{Name: fs.Arg(0), Persona: *persona, Provider: *prov, Model: *model}, nil
}

func cmdNew(root string, args []string) error {
	opt, err := parseNew(args)
	if err != nil {
		return err
	}
	if (opt.Provider == "") != (opt.Model == "") {
		return errors.New("pass both --provider and --model, or neither")
	}
	if opt.Provider != "" {
		if _, _, err := config.Lookup(root, opt.Provider, opt.Model); err != nil {
			return err
		}
	}
	b, err := bot.Create(root, opt.Name, opt.Persona, defaultLinks)
	if err != nil {
		return err
	}
	if opt.Provider != "" {
		if err := b.SetBinding(opt.Provider, opt.Model); err != nil {
			return err
		}
	}
	if err := b.WriteRules(kernel.DefaultRules()); err != nil {
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
	if err := b.SetOwners([]string{who.ID}); err != nil {
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
		extra := ""
		if cfg.Provider != "" {
			extra = "\t" + cfg.Provider + "/" + cfg.Model
		}
		fmt.Printf("%s\t%s%s\n", cfg.Name, strings.Join(cfg.Links, ","), extra)
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
	b, err := bot.Load(root, opt.Name)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pr := &printer{yes: opt.Yes, reader: bufio.NewReader(os.Stdin)}
	k, who, err := openKernel(root, b, opt.MaxSteps, pr.Approve, pr)
	if err != nil {
		return err
	}
	lock, err := b.LockHandle()
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := showNotes(b); err != nil {
		return err
	}
	box := k.Inbox
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
		if err := showNotes(b); err != nil {
			return err
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

func openKernel(root string, b *bot.Bot, steps int, approve func(context.Context, string, string) (bool, error), pr *printer) (*kernel.Kernel, person.Person, error) {
	if !b.Grants(link.NameTerminal) && pr != nil {
		return nil, person.Person{}, fmt.Errorf("bot %q does not grant the terminal link", b.Name())
	}
	links, err := mount(b)
	if err != nil {
		return nil, person.Person{}, err
	}
	var who person.Person
	if pr != nil {
		ext, display, err := link.Account()
		if err != nil {
			return nil, person.Person{}, err
		}
		who, err = person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
		if err != nil {
			return nil, person.Person{}, err
		}
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return nil, person.Person{}, err
	}
	resolved, contextLen, err := config.Resolve(root, b.Config().Provider, b.Config().Model, os.Getenv)
	if err != nil {
		return nil, person.Person{}, err
	}
	if resolved.APIKey == "" {
		return nil, person.Person{}, fmt.Errorf("%s is empty", resolved.KeyEnv)
	}
	client := &provider.Client{BaseURL: resolved.BaseURL, APIKey: resolved.APIKey, Model: resolved.Model}
	budget := kernel.BudgetFor(b.Config())
	if b.Config().ContextLimit == 0 && contextLen > 0 {
		budget = kernel.BudgetFor(bot.Config{ContextLimit: contextLen})
	}
	k := &kernel.Kernel{
		Bot:       b,
		Provider:  client,
		Selector:  provider.FixedSelector{P: client},
		Links:     links,
		Inbox:     box,
		Memory:    memory.Open(root),
		Compactor: compact.LLM{P: client},
		Extractor: kernel.LLMExtractor{P: client},
		Approve:   approve,
		MaxSteps:  steps,
		Budget:    budget,
	}
	if pr != nil {
		k.OnDelta = pr.Delta
		k.OnTool = pr.Tool
		k.OnResult = pr.Result
	}
	return k, who, nil
}

func mount(b *bot.Bot) ([]link.Link, error) {
	sender := "timer"
	if owners := b.Config().Owners; len(owners) > 0 {
		sender = owners[0]
	}
	var out []link.Link
	seen := map[string]bool{}
	for _, name := range b.Config().Links {
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
			out = append(out, &link.Timer{Dir: b.RoutineDir(), Sender: sender})
		case link.NameMemory:
			out = append(out, link.Memory{})
		case link.NamePersona:
			out = append(out, link.Persona{})
		case link.NameContext:
			out = append(out, link.Context{})
		default:
			return nil, fmt.Errorf("unknown link %q", name)
		}
	}
	return out, nil
}

func showNotes(b *bot.Bot) error {
	box := notify.Open(b.OutboxPath())
	notes, err := box.Unread()
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return nil
	}
	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		fmt.Fprintf(os.Stderr, "notification: %s\n", n.Text)
		ids = append(ids, n.ID)
	}
	return box.MarkRead(ids)
}

func cmdBind(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"provider": true, "model": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("bind", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	prov := fs.String("provider", "", "named provider")
	model := fs.String("model", "", "model id")
	clear := fs.Bool("clear", false, "drop the binding and use environment defaults")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: xbot bind <name> --provider NAME --model NAME")
	}
	b, err := bot.Load(root, fs.Arg(0))
	if err != nil {
		return err
	}
	if *clear {
		if err := b.SetBinding("", ""); err != nil {
			return err
		}
		fmt.Printf("cleared provider binding for %s\n", b.Name())
		return nil
	}
	if *prov == "" || *model == "" {
		return errors.New("usage: xbot bind <name> --provider NAME --model NAME")
	}
	if _, _, err := config.Lookup(root, *prov, *model); err != nil {
		return err
	}
	if err := b.SetBinding(*prov, *model); err != nil {
		return err
	}
	fmt.Printf("bound %s to %s/%s\n", b.Name(), *prov, *model)
	return nil
}

func cmdServe(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"interval": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	every := fs.Duration("interval", 15*time.Second, "how often to poll")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot serve")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	deny := func(context.Context, string, string) (bool, error) { return false, nil }
	for {
		err := serve.Pass(ctx, root, time.Now(), func(b *bot.Bot) (*kernel.Kernel, error) {
			k, _, err := openKernel(root, b, 8, deny, nil)
			return k, err
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "xbot:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*every):
		}
	}
}

func cmdProvider(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot provider add|list|remove")
	}
	switch args[0] {
	case "list":
		f, err := config.Load(root)
		if err != nil {
			return err
		}
		if len(f.Providers) == 0 {
			fmt.Println("no providers")
			return nil
		}
		for _, p := range f.Providers {
			fmt.Printf("%s\t%s\tkey=%s\n", p.Name, p.BaseURL, p.APIKeyEnv)
			for _, m := range p.Models {
				fmt.Printf("  %s\tcontext=%d\n", m.Name, m.Context)
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
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("usage: xbot provider add <name> --base-url URL --api-key-env VAR --model NAME --context N")
		}
		if err := config.Add(root, config.Provider{
			Name: fs.Arg(0), BaseURL: *base, APIKeyEnv: *keyEnv,
		}, config.ModelSpec{Name: *model, Context: *contextLen}); err != nil {
			return err
		}
		fmt.Printf("saved provider %s\n", fs.Arg(0))
		return nil
	default:
		return fmt.Errorf("unknown provider command %q", args[0])
	}
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
