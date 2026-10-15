package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/person"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/serve"
	"xbot/internal/skill"
	"xbot/internal/tools"
	"xbot/internal/work"
)

const usageText = `XBot is a personal bot. One bot has one continuous conversation.

Usage:
  xbot [--root DIR] new <name> [--persona TEXT] [--provider NAME] [--model NAME]
  xbot [--root DIR] list
  xbot [--root DIR] chat <name> [--yes] [--max-steps N]
  xbot [--root DIR] bind <name> --provider NAME --model NAME
  xbot [--root DIR] bind <name> --clear
  xbot [--root DIR] serve [--interval 15s] [--http 127.0.0.1:8787]
  xbot [--root DIR] runner [--addr 127.0.0.1:18771] [--token TOKEN]
  xbot [--root DIR] webhook add <bot> <name> [--auth hmac|bearer] [--public-url URL]
  xbot [--root DIR] webhook list <bot>
  xbot [--root DIR] webhook remove <bot> <name>
  xbot [--root DIR] watch add <bot> <id> --path PATTERN
  xbot [--root DIR] watch list <bot>
  xbot [--root DIR] watch remove <bot> <id>
  xbot [--root DIR] provider add <name> --base-url URL --api-key-env VAR --model NAME --context N
  xbot [--root DIR] provider list
  xbot [--root DIR] provider remove <name> [--model NAME]

Environment:
  XBOT_API_KEY    API key when a bot has no provider binding
  XBOT_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com/v1)
  XBOT_MODEL       Model name (default deepseek-chat)
  XBOT_PUBLIC_URL  Base URL printed for new webhooks (default http://127.0.0.1:8787)

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
	case "runner":
		return cmdRunner(rest[1:])
	case "provider":
		return cmdProvider(root, rest[1:])
	case "webhook":
		return cmdWebhook(root, rest[1:])
	case "watch":
		return cmdWatch(root, rest[1:])
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
	link.NameAgents, link.NameSkills,
	link.NameWebhook, link.NameWatch,
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
	Name        string
	Yes         bool
	MaxSteps    int
	RunnerURL   string
	RunnerToken string
}

func parseChat(args []string) (chatOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"max-steps": true, "runner": true, "runner-token": true})
	if err != nil {
		return chatOpts{}, err
	}
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "approve shell commands and file writes without prompting")
	steps := fs.Int("max-steps", 8, "maximum model calls per wake")
	runnerURL := fs.String("runner", "", "remote executor base URL")
	runnerToken := fs.String("runner-token", "", "remote executor token")
	if err := fs.Parse(ordered); err != nil {
		return chatOpts{}, err
	}
	if fs.NArg() != 1 {
		return chatOpts{}, errors.New("usage: xbot chat <name>")
	}
	return chatOpts{Name: fs.Arg(0), Yes: *yes, MaxSteps: *steps, RunnerURL: *runnerURL, RunnerToken: *runnerToken}, nil
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
	k, who, err := openKernel(root, b, opt.MaxSteps, pr.Approve, pr, opt.Yes, runnerSpec{opt.RunnerURL, opt.RunnerToken})
	if err != nil {
		return err
	}
	lock, err := b.LockHandle()
	if err != nil {
		return err
	}
	defer lock.Release()
	lines := make(chan proc.Line)
	go func() {
		defer close(lines)
		for {
			line, err := pr.reader.ReadString('\n')
			lines <- proc.Line{Text: line, Err: err}
			if err != nil || isQuit(line) {
				return
			}
		}
	}()
	return proc.Run(ctx, proc.Config{
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
			ev := link.TerminalEvent(who.ID, strings.TrimSpace(text), time.Now())
			if err := k.Inbox.Post(ev); err != nil {
				return link.Event{}, err
			}
			return ev, nil
		},
		Idle: func() {
			if err := showNotes(b); err != nil {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
			fmt.Fprintf(os.Stderr, "%s › ", b.Name())
		},
		After: func(_ link.Event, err error) {
			pr.Break()
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
			if err := showNotes(b); err != nil {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
		},
	})
}

type runnerSpec struct {
	URL   string
	Token string
}

func openKernel(root string, b *bot.Bot, steps int, approve func(context.Context, string, string) (bool, error), pr *printer, allowBackground bool, runner runnerSpec) (*kernel.Kernel, person.Person, error) {
	if !b.Grants(link.NameTerminal) && pr != nil {
		return nil, person.Person{}, fmt.Errorf("bot %q does not grant the terminal link", b.Name())
	}
	links, err := mount(b, runner)
	if err != nil {
		return nil, person.Person{}, err
	}
	workDir, sharedDir, err := work.Open(root, b.Name())
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
		Bot:             b,
		Provider:        client,
		Selector:        provider.FixedSelector{P: client},
		Links:           links,
		Inbox:           box,
		Memory:          memory.Open(root),
		Compactor:       compact.LLM{P: client},
		Extractor:       kernel.LLMExtractor{P: client},
		Approve:         approve,
		MaxSteps:        steps,
		Budget:          budget,
		AllowBackground: allowBackground,
		WorkDir:         workDir,
		SharedDir:       sharedDir,
		SkillsDir:       skill.Dir(root),
	}
	if pr != nil {
		k.OnDelta = pr.Delta
		k.OnTool = pr.Tool
		k.OnResult = pr.Result
	}
	return k, who, nil
}

func mount(b *bot.Bot, runner runnerSpec) ([]link.Link, error) {
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
			dir, shared, err := work.Open(b.Root, b.Name())
			if err != nil {
				return nil, err
			}
			out = append(out, link.NewToolsLinkAt(dir, shared))
		case link.NameTimer:
			out = append(out, &link.Timer{Dir: b.RoutineDir(), Sender: sender})
		case link.NameMemory:
			out = append(out, link.Memory{})
		case link.NamePersona:
			out = append(out, link.Persona{})
		case link.NameContext:
			out = append(out, link.Context{})
		case link.NameAgents:
			out = append(out, link.Agents{})
		case link.NameSkills:
			out = append(out, link.Skills{})
		case link.NameWebhook:
			out = append(out, link.Webhook{})
		case link.NameWatch:
			out = append(out, link.Watch{})
		case link.NameRunner:
			url, token := runner.URL, runner.Token
			if url == "" {
				url = os.Getenv("XBOT_RUNNER_URL")
				token = os.Getenv("XBOT_RUNNER_TOKEN")
			}
			if url != "" && token == "" {
				return nil, errors.New("runner token is empty")
			}
			out = append(out, link.Runner{BaseURL: url, Token: token})
		default:
			return nil, fmt.Errorf("unknown link %q", name)
		}
	}
	if runner.URL == "" {
		runner.URL = os.Getenv("XBOT_RUNNER_URL")
		runner.Token = os.Getenv("XBOT_RUNNER_TOKEN")
	}
	if runner.URL != "" && !seen[link.NameRunner] {
		if runner.Token == "" {
			return nil, errors.New("runner token is empty")
		}
		out = append(out, link.Runner{BaseURL: runner.URL, Token: runner.Token})
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
	ordered, err := reorderFlags(args, map[string]bool{"interval": true, "http": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	every := fs.Duration("interval", 15*time.Second, "how often to handle inboxes")
	httpAddr := fs.String("http", "127.0.0.1:8787", "webhook listen address")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot serve [--interval 15s] [--http 127.0.0.1:8787]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	hub := &inbound.Server{Root: root, Window: inbound.DefaultWindow, Debounce: inbound.DefaultDebounce}
	ln, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "webhook listening on %s\n", ln.Addr())
	srv := &http.Server{Handler: hub.Handler()}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "xbot:", err)
		}
	}()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				if err := hub.Flush(now); err != nil {
					fmt.Fprintln(os.Stderr, "xbot:", err)
				}
				if err := inbound.DeliverAll(root, now, hub.Debounce); err != nil {
					fmt.Fprintln(os.Stderr, "xbot:", err)
				}
			}
		}
	}()
	deny := func(context.Context, string, string) (bool, error) { return false, nil }
	for {
		err := serve.Pass(ctx, root, time.Now(), func(b *bot.Bot) (*kernel.Kernel, error) {
			k, _, err := openKernel(root, b, 8, deny, nil, false, runnerSpec{})
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

func cmdWebhook(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot webhook add|list|remove")
	}
	switch args[0] {
	case "add":
		return cmdWebhookAdd(root, args[1:])
	case "list":
		return cmdWebhookList(root, args[1:])
	case "remove":
		return cmdWebhookRemove(root, args[1:])
	default:
		return fmt.Errorf("unknown webhook command %q", args[0])
	}
}

func cmdWebhookAdd(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"auth": true, "public-url": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("webhook add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	auth := fs.String("auth", inbound.AuthHMAC, "hmac or bearer")
	public := fs.String("public-url", "", "base URL printed with the secret")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: xbot webhook add <bot> <name>")
	}
	b, err := bot.Load(root, fs.Arg(0))
	if err != nil {
		return err
	}
	h, err := inbound.AddHook(b, fs.Arg(1), *auth, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("url: %s\nsecret: %s\nauth: %s\n", inbound.WebhookURL(publicBase(*public), b.Name(), h.Name), h.Secret, h.Auth)
	return nil
}

func cmdWebhookList(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot webhook list <bot>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	hooks, err := inbound.ListHooks(b)
	if err != nil {
		return err
	}
	base := publicBase("")
	if len(hooks) == 0 {
		fmt.Println("no webhooks")
		return nil
	}
	for _, h := range hooks {
		fmt.Printf("%s\t%s\t%s\n", h.Name, h.Auth, inbound.WebhookURL(base, b.Name(), h.Name))
	}
	return nil
}

func cmdWebhookRemove(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot webhook remove <bot> <name>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := inbound.RemoveHook(b, args[1]); err != nil {
		return err
	}
	fmt.Printf("removed webhook %s\n", args[1])
	return nil
}

func publicBase(flag string) string {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimRight(strings.TrimSpace(flag), "/")
	}
	if v := strings.TrimSpace(os.Getenv("XBOT_PUBLIC_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8787"
}

func cmdWatch(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot watch add|list|remove")
	}
	switch args[0] {
	case "add":
		return cmdWatchAdd(root, args[1:])
	case "list":
		return cmdWatchList(root, args[1:])
	case "remove":
		return cmdWatchRemove(root, args[1:])
	default:
		return fmt.Errorf("unknown watch command %q", args[0])
	}
}

func cmdWatchAdd(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"path": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("watch add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pattern := fs.String("path", "", "absolute glob")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 2 || *pattern == "" {
		return errors.New("usage: xbot watch add <bot> <id> --path PATTERN")
	}
	b, err := bot.Load(root, fs.Arg(0))
	if err != nil {
		return err
	}
	w, err := inbound.AddWatch(b, fs.Arg(1), *pattern)
	if err != nil {
		return err
	}
	fmt.Printf("watch %s %s\n", w.ID, w.Glob)
	return nil
}

func cmdWatchList(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot watch list <bot>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	watches, err := inbound.ListWatches(b)
	if err != nil {
		return err
	}
	if len(watches) == 0 {
		fmt.Println("no watches")
		return nil
	}
	for _, w := range watches {
		fmt.Printf("%s\t%s\n", w.ID, w.Glob)
	}
	return nil
}

func cmdWatchRemove(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot watch remove <bot> <id>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := inbound.RemoveWatch(b, args[1]); err != nil {
		return err
	}
	fmt.Printf("removed watch %s\n", args[1])
	return nil
}

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
	loc.ToolSet = func(string) []tools.Tool {
		return []tools.Tool{tools.NewShell(), &tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}}
	}
	return exec.Listen(*addr, tok, loc)
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
