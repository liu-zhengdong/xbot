package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/code"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/desktop"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/nodesync"
	"xbot/internal/notify"
	"xbot/internal/person"
	"xbot/internal/provider"
	"xbot/internal/serve"
	"xbot/internal/skill"
	"xbot/internal/tunnel"
	"xbot/internal/vault"
	"xbot/internal/web"
	"xbot/internal/work"
)

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
	resolved, contextLen, err := config.Resolve(root, b.Config().Provider, b.Config().Model, config.Getenv)
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
	for _, item := range links {
		coding, ok := item.(*code.Link)
		if !ok {
			continue
		}
		coding.UseProvider(client)
		if runner.URL != "" {
			coding.UseRunner(runner.URL, runner.Token)
		}
		coding.UseTranscript(func(text string) error {
			return b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: text}})
		})
		if cmd := code.ResolveCommand(os.Getenv("XBOT_CODE_AGENT")); cmd != "" {
			coding.UseCommand(cmd)
		}
	}
	store, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		return nil, person.Person{}, err
	}
	if local, ok := store.(*vault.Store); ok {
		vault.Register(local)
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
		Vault:           store,
		Redact:          store.Redact,
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
		case link.NameWeb:
			out = append(out, link.Web{})
		case link.NamePeers:
			out = append(out, link.Peers{})
		case link.NameTelegram:
			out = append(out, link.NewConnector(bot.Dir(b.Root, b.Name()), b.SecretPath(), link.NameTelegram))
		case link.NameLark:
			out = append(out, link.NewConnector(bot.Dir(b.Root, b.Name()), b.SecretPath(), link.NameLark))
		case link.NameCoding:
			out = append(out, code.Open(bot.Dir(b.Root, b.Name()), b.InboxPath()))
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
		case link.NameNode:
			out = append(out, node.Open(b.Root))
		case link.NameDesktop:
			out = append(out, desktop.Open(b.Root))
		case link.NameBrowser:
			out = append(out, browser.Open(b.Root))
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
	servers, err := link.LoadMCP(b.MCPPath())
	if err != nil {
		return nil, err
	}
	for i := range servers {
		out = append(out, link.NewMCP(servers[i]))
	}
	return out, nil
}

// startEmitters keeps one long-lived kernel for each bot that has an MCP server
// so notifications reach the inbox while serve is up. The kernel serve.Pass
// builds every interval is short-lived and must not open another connection.
// The first server is picked up on a later pass. After that, mcp.json edits
// need a restart.
func startEmitters(ctx context.Context, root string, seen map[string]bool) {
	if ctx.Err() != nil {
		return
	}
	cfgs, err := bot.List(root)
	if err != nil {
		return
	}
	for _, cfg := range cfgs {
		if seen[cfg.Name] {
			continue
		}
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			continue
		}
		servers, err := link.LoadMCP(b.MCPPath())
		if err != nil || len(servers) == 0 {
			continue
		}
		k, _, err := openKernel(root, b, 8, nil, nil, false, runnerSpec{})
		if err != nil {
			continue
		}
		k.StartEmitters(ctx)
		seen[cfg.Name] = true
	}
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
	uncertified := fs.Bool("uncertified", false, "allow a model that has not passed the proactivity suite")
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
	if err := config.AllowBind(root, *prov, *model, *uncertified); err != nil {
		return err
	}
	if err := b.SetBinding(*prov, *model); err != nil {
		return err
	}
	fmt.Printf("bound %s to %s/%s\n", b.Name(), *prov, *model)
	return nil
}

func cmdServe(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"interval": true, "http": true, "tunnel": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	every := fs.Duration("interval", 15*time.Second, "how often to handle inboxes")
	httpAddr := fs.String("http", "127.0.0.1:8787", "webhook listen address")
	tunnelMode := fs.String("tunnel", "", "cloudflare or tailscale")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot serve [--interval 15s] [--http 127.0.0.1:8787] [--tunnel cloudflare|tailscale]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	resolved, err := tunnel.Resolve(ctx, *tunnelMode, *httpAddr, os.Getenv("XBOT_TAILSCALE"))
	if err != nil {
		return err
	}
	*httpAddr = resolved.ListenAddr
	hub := &inbound.Server{Root: root, Window: inbound.DefaultWindow, Debounce: inbound.DefaultDebounce}
	ui := &web.Server{
		Root:         root,
		DefaultLinks: defaultLinks,
		ForkTurns:    true,
		NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) {
			k, _, err := openKernel(root, b, 8, nil, nil, false, runnerSpec{})
			return k, err
		},
	}
	token, _, err := ui.EnsureToken()
	if err != nil {
		return err
	}
	if err := ensureSeed(root); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	hub.Register(mux)
	ui.Register(mux)
	fmt.Fprintf(os.Stderr, "listening on http://%s\n", ln.Addr())
	publicURL := resolved.PublicURL
	var cloud *tunnel.Proc
	if resolved.Mode == tunnel.Cloudflare {
		cloud, err = tunnel.StartCloudflare(ctx, tunnel.LocalURL(ln.Addr()), os.Getenv("XBOT_CLOUDFLARED"), 45*time.Second)
		if err != nil {
			_ = ln.Close()
			return err
		}
		defer cloud.Close()
		publicURL = cloud.PublicURL
	}
	if publicURL != "" {
		fmt.Fprintf(os.Stderr, "public url: %s\n", publicURL)
	}
	fmt.Fprintf(os.Stderr, "login token: %s\n", token)
	nodesync.Start(ctx, root, nodesync.DefaultEvery, nodesync.DefaultDebounce)
	srv := &http.Server{Handler: mux}
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
	browser.TieLaunch(true)
	defer browser.TieLaunch(false)
	defer func() { _ = browser.ReleaseTied(root) }()
	seen := map[string]bool{}
	for {
		startEmitters(ctx, root, seen)
		err := serve.Pass(ctx, root, time.Now(), func(b *bot.Bot) (*kernel.Kernel, error) {
			k, _, err := openKernel(root, b, 8, nil, nil, false, runnerSpec{})
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
