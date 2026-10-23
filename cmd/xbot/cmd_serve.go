package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"xbot/internal/appshell"
	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/clock"
	"xbot/internal/code"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/desktop"
	"xbot/internal/fsutil"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/permissions"
	"xbot/internal/person"
	"xbot/internal/provider"
	"xbot/internal/remote"
	"xbot/internal/serve"
	"xbot/internal/skill"
	"xbot/internal/tunnel"
	"xbot/internal/upgrade"
	"xbot/internal/vault"
	"xbot/internal/web"
	"xbot/internal/work"
)

// serveSteps is what the serve path passes to openKernel: 0 lets the bot role
// decide the step cap (chat 8, dev 24) instead of pinning one here.
const serveSteps = 0

// legacyNodeGrant is the retired node link's name. It is no longer a link, but
// an old bot config may still list it; serve skips it with one notice.
const legacyNodeGrant = "node"

// newServeKernel opens a kernel the way the serve path does, so every caller
// there shares one definition of what a served bot gets.
// loginTokenLine is the startup line with the web 登录码. It keeps "login token"
// because log filters and docs search for that text.
const loginTokenLine = "登录码（login token）: %s\n"

func newServeKernel(root string, b *bot.Bot) (*kernel.Kernel, error) {
	k, _, err := openKernel(root, b, serveSteps, nil, nil, false, runnerSpec{})
	return k, err
}

func openKernel(root string, b *bot.Bot, steps int, approve func(context.Context, string, string) (bool, error), pr *printer, allowBackground bool, runner runnerSpec) (*kernel.Kernel, person.Person, error) {
	// Migrate always returns nil. A bad legacy directory is logged and left in
	// place; it must not stop the kernel from opening.
	_ = memory.Migrate(root)
	if !b.Grants(link.NameTerminal) && pr != nil {
		return nil, person.Person{}, fmt.Errorf("bot %q does not grant the terminal link", b.Name())
	}
	store, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		return nil, person.Person{}, err
	}
	links, err := mount(b, runner)
	if err != nil {
		return nil, person.Person{}, err
	}
	// First access on a mounted connector migrates any legacy plaintext token
	// into the vault and switches the connector to placeholders.
	attachConnectors(links, store)
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
	resolved, err := resolveBotModel(root, b.Config())
	if err != nil {
		return nil, person.Person{}, err
	}
	client := &provider.Client{BaseURL: resolved.BaseURL, APIKey: resolved.APIKey, Model: resolved.Model, Thinking: resolved.Thinking}
	// The kernel derives both context lines from the bound model on each use,
	// so a model change moves them; the budget only carries the tail floor.
	budget := kernel.BudgetFor(b.Config())
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
			return b.Append(bot.Entry{Origin: bot.OriginOwner, Message: provider.Message{Role: provider.RoleUser, Content: text}})
		})
		if cmd := code.ResolveCommand(os.Getenv("XBOT_CODE_AGENT")); cmd != "" {
			coding.UseCommand(cmd)
		}
	}
	if local, ok := store.(*vault.Store); ok {
		vault.Register(local)
	}
	// The bot's role picks the working defaults: how long a reply may stay on the
	// main line, and how long a background task may keep working. An explicit
	// --max-steps still bounds one run for evals and tests that want it short.
	work := b.Config().Working()
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
		// The foreground understands and hands out work; typed subagents do
		// it (ADR 0016).
		Dispatch: true,
		// No permission cards unless the owner turned them on (ADR 0019).
		PermissionsOn: func() bool { return permissions.On(root) },
		// Foreground limits come from the role unless the bot overrode them.
		ForegroundMaxTools: work.ForegroundMaxTools,
		ForegroundMaxTime:  work.ForegroundMaxTime,
		// How long one background task may keep going.
		BackgroundMaxTime: work.BackgroundMaxTime,
		WorkDir:           workDir,
		SharedDir:         sharedDir,
		SkillsDir:         skill.Dir(root),
		Vault:             store,
		Redact:            store.Redact,
	}
	if pr != nil {
		k.OnDelta = pr.Delta
		k.OnTool = pr.Tool
		k.OnResult = pr.Result
	}
	if err := k.RecoverTasks(); err != nil {
		return nil, person.Person{}, err
	}
	return k, who, nil
}

// attachConnectors hands the vault to every connector link and runs the
// first-access migration. A backend the connector refuses leaves it on the
// legacy file, so the migration retries on the next start.
func attachConnectors(links []link.Link, backend vault.Backend) {
	if backend == nil {
		return
	}
	for _, l := range links {
		c, ok := l.(*link.Connector)
		if !ok {
			continue
		}
		if err := c.UseVault(backend); err != nil {
			// The legacy plaintext stays in place and the migration retries on
			// the next start. The error carries no value.
			log.Printf("connector %s: credential migration into the vault failed, retrying next start: %v", c.Kind, err)
		}
	}
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
		case legacyNodeGrant:
			// The node feature is gone; an old grant mounts nothing and must not
			// fail the bot. One notice per bot and process keeps restarts quiet.
			if noteLegacyNodeGrant(b.Name()) {
				fmt.Fprintf(os.Stderr, "xbot: bot %s: 节点功能已移除，忽略 node 授权\n", b.Name())
			}
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

// legacyNodeFiles names the retired node feature's files under root, in the
// order the notice lists them. Serve only reports them; nothing is deleted.
func legacyNodeFiles(root string) []string {
	var found []string
	for _, name := range []string{"nodes.json", "node-token", "node-pending"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

var (
	legacyNodeGrants   = map[string]bool{}
	legacyNodeGrantsMu sync.Mutex
)

// noteLegacyNodeGrant reports true the first time a bot's node grant is skipped.
func noteLegacyNodeGrant(name string) bool {
	legacyNodeGrantsMu.Lock()
	defer legacyNodeGrantsMu.Unlock()
	if legacyNodeGrants[name] {
		return false
	}
	legacyNodeGrants[name] = true
	return true
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
		k, err := newServeKernel(root, b)
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
	noAutoUpgrade := fs.Bool("no-auto-upgrade", false, "do not check for a newer xbot release")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: xbot serve [--interval 15s] [--http 127.0.0.1:8787] [--tunnel cloudflare|tailscale] [--no-auto-upgrade]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals()...)
	defer stop()
	return runServe(ctx, root, serveConfig{HTTPAddr: *httpAddr, Interval: *every, Tunnel: *tunnelMode, AutoUpgrade: !*noAutoUpgrade && os.Getenv("XBOT_AUTO_UPGRADE") != "0"})
}

type serveConfig struct {
	HTTPAddr    string
	Interval    time.Duration
	Tunnel      string
	AutoUpgrade bool
	App         bool
	OnListening func(ui *web.Server, addr net.Addr)
	// DeliverTimers replaces serve.DeliverTimers; nil uses it.
	DeliverTimers func(root string, now time.Time) error
}

// resolveBotModel is shared by startup preflight and the actual kernel path.
func resolveBotModel(root string, cfg bot.Config) (config.ModelConfig, error) {
	model, _, err := config.Resolve(root, cfg.Provider, cfg.Model, config.Getenv)
	if err != nil {
		return config.ModelConfig{}, fmt.Errorf("bot %s: %w", cfg.Name, err)
	}
	if model.APIKey == "" {
		return config.ModelConfig{}, fmt.Errorf("bot %s: %s is empty", cfg.Name, model.KeyEnv)
	}
	return model, nil
}

func checkServeModels(root string) error {
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	for _, cfg := range cfgs {
		if _, err := resolveBotModel(root, cfg); err != nil {
			return err
		}
	}
	return nil
}

func runServe(ctx context.Context, root string, cfg serveConfig) error {
	lock, ok, err := fsutil.TryLock(filepath.Join(root, "serve.lock"))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("xbot serve is already running for this data root")
	}
	defer lock.Release()

	gatewayToken := os.Getenv("XBOT_GATEWAY_TOKEN")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Tickers that write the data root are waited for on return, after the
	// context ends, so nothing writes the root once serve has stopped.
	var tickers sync.WaitGroup
	defer func() { cancel(); tickers.Wait() }()
	// The updater downloads in the background and, on the owner's click,
	// installs and starts the new version. The desktop app restarts its
	// window; serve re-execs this process.
	updater := upgrade.NewUpdater(root)
	deliverTimers := cfg.DeliverTimers
	if deliverTimers == nil {
		deliverTimers = serve.DeliverTimers
	}
	if cfg.App {
		cfg.Tunnel = ""
	}
	resolved, err := tunnel.Resolve(ctx, cfg.Tunnel, cfg.HTTPAddr, os.Getenv("XBOT_TAILSCALE"))
	if err != nil {
		return err
	}
	cfg.HTTPAddr = resolved.ListenAddr
	hub := &inbound.Server{Root: root, Window: inbound.DefaultWindow, Debounce: inbound.DefaultDebounce, OpenVault: func(b *bot.Bot) (vault.Backend, error) {
		return vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
	}}
	ui := &web.Server{
		Root:         root,
		DefaultLinks: defaultLinks,
		// Interjections no longer fork the running turn (ADR 0016): the
		// foreground's turns are short and the work runs in subagents.
		ForkTurns: false,
		App:       cfg.App,
		NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) {
			return newServeKernel(root, b)
		},
		Updater: updater,
	}
	if cfg.App {
		// Only the desktop app can open a folder in the file manager.
		ui.OpenPath = appshell.OpenPath
	}
	token, _, err := ui.EnsureToken()
	if err != nil {
		return err
	}
	// The app binds before touching state, so a second instance on a busy
	// port does not sweep the running server's approval cards.
	var ln net.Listener
	if cfg.App {
		if ln, err = net.Listen("tcp", cfg.HTTPAddr); err != nil {
			return err
		}
		defer ln.Close()
	}
	// Cards raised before a restart only lived in memory, so nobody can decide
	// them any more: record them as no-decision rather than leaving a card that
	// looks pending and can never be answered.
	if closed, cerr := ui.ResolveRestartOrphans(); cerr != nil {
		fmt.Fprintf(os.Stderr, "xbot: approval sweep: %v\n", cerr)
	} else if note := web.DescribeOrphans(closed); note != "" {
		fmt.Fprintln(os.Stderr, "xbot:", note)
	}
	if err := ensureSeed(root); err != nil {
		return err
	}
	// The serve command has no key-entry screen: fail before accepting work.
	// The desktop app must still reach its existing first-run key setup screen.
	if !cfg.App {
		if err := checkServeModels(root); err != nil {
			return err
		}
	}
	migrateServeRoutines(root)
	// One synchronous check before the server and the bot pass start, so a
	// notice for slots missed while down is computed before anything runs them.
	if err := deliverTimers(root, clock.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "xbot: routines:", err)
	}
	if ln == nil {
		if ln, err = net.Listen("tcp", cfg.HTTPAddr); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		_ = ln.Close()
		return err
	}
	argv := os.Args
	env := os.Environ()
	_ = os.Unsetenv("XBOT_GATEWAY_TOKEN")
	// restart asks the serve loop to re-exec. A graceful upgrade and SIGHUP both
	// funnel through it so the listener stays up until the last moment.
	restartCh := make(chan string, 1)
	restart := func(reason string) {
		select {
		case restartCh <- reason:
		default:
		}
	}
	// Installing an update ends this way in both modes: the window reopens
	// (appshell.Relaunch), or serve re-execs the new binary.
	if cfg.App {
		// Back to the same binary by the same arguments, once this process
		// has let go of the data root.
		// argv[0] is this binary, which the helper already puts first.
		updater.Relaunch = func() {
			if err := appshell.Relaunch(exe, argv[1:], config.HeldEnv(env)); err != nil {
				fmt.Fprintln(os.Stderr, "xbot: relaunch:", err)
			}
		}
	} else {
		updater.Relaunch = func() { restart("updated") }
	}
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		select {
		case <-ctx.Done():
		case <-hup:
			restart("SIGHUP")
		}
	}()
	if cfg.AutoUpgrade {
		go updater.Run(ctx)
	}
	if pending := upgrade.Pending(root); pending != "" {
		fmt.Fprintf(os.Stderr, "xbot: %s 已下载，重启后生效（网页右上角「更新」）\n", pending)
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
	if !cfg.App {
		fmt.Fprintf(os.Stderr, loginTokenLine, token)
	}
	if legacy := legacyNodeFiles(root); len(legacy) > 0 {
		fmt.Fprintln(os.Stderr, "xbot: 节点功能已移除，忽略数据目录里的旧节点文件（nodes.json、node-token、node-pending），文件保留不删")
	}
	// Waited for on return, so nothing writes the data root after serve stops.
	// remote cannot import kernel, so serve hands it the reply reader.
	remote.TurnReply = kernel.ReplyAfter
	remoteCtx, stopRemote := context.WithCancel(ctx)
	remoteDone := make(chan struct{})
	go func() {
		defer close(remoteDone)
		remote.Run(remoteCtx, root, gatewayToken, func(f string, a ...any) { fmt.Fprintf(os.Stderr, "xbot: remote: "+f+"\n", a...) })
	}()
	defer func() { stopRemote(); <-remoteDone }()
	// Routines get their own ticker, with a fresh now each tick, so a one-shot
	// does not wait behind every bot's model calls in serve.Pass.
	tickers.Add(1)
	go func() {
		defer tickers.Done()
		tick := time.NewTicker(serve.TimerEvery)
		defer tick.Stop()
		for {
			if err := deliverTimers(root, clock.Now()); err != nil {
				fmt.Fprintln(os.Stderr, "xbot: routines:", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "xbot:", err)
			if cfg.App {
				// The window is useless without the UI; stop and close it.
				cancel()
			}
		}
	}()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if cfg.OnListening != nil {
		cfg.OnListening(ui, ln.Addr())
	}
	tickers.Add(1)
	go func() {
		defer tickers.Done()
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
	// A cloudflare tunnel child should not outlive the process we re-exec into.
	var cleanup func()
	if cloud != nil {
		cleanup = func() { cloud.Close() }
	}
	for {
		select {
		case reason := <-restartCh:
			if cfg.App {
				return nil
			}
			return reexecService(ln, ui, exe, argv, env, reason, cleanup)
		default:
		}
		startEmitters(ctx, root, seen)
		err := serve.Pass(ctx, root, clock.Now(), func(b *bot.Bot) (*kernel.Kernel, error) {
			return newServeKernel(root, b)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "xbot:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case reason := <-restartCh:
			if cfg.App {
				return nil
			}
			return reexecService(ln, ui, exe, argv, env, reason, cleanup)
		case <-time.After(cfg.Interval):
		}
	}
}

// migrateServeRoutines moves legacy UTC routines to the computer's zone once.
// A bad file is logged and skipped; it must not stop serve.
func migrateServeRoutines(root string) {
	cfgs, err := bot.List(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xbot: routines:", err)
		return
	}
	now := time.Now()
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xbot: routines: %s: %v\n", cfg.Name, err)
			continue
		}
		migs, err := link.MigrateRoutines(b.RoutineDir(), time.Local, now, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xbot: routines: %s: %v\n", cfg.Name, err)
			continue
		}
		for _, m := range migs {
			if !m.Changed {
				if strings.HasPrefix(m.Note, "skipped") {
					fmt.Fprintf(os.Stderr, "routines: %s/%s %s\n", cfg.Name, m.ID, m.Note)
				}
				continue
			}
			if m.OldCron != "" {
				if m.NewCron == m.OldCron {
					fmt.Fprintf(os.Stderr, "routines: %s/%s cron %q: %s\n", cfg.Name, m.ID, m.OldCron, m.Note)
				} else if m.NewZone == "UTC" {
					fmt.Fprintf(os.Stderr, "routines: %s/%s cron %q UTC -> kept UTC (no exact local equivalent)\n", cfg.Name, m.ID, m.OldCron)
				} else {
					fmt.Fprintf(os.Stderr, "routines: %s/%s cron %q UTC -> %q local\n", cfg.Name, m.ID, m.OldCron, m.NewCron)
				}
			} else {
				fmt.Fprintf(os.Stderr, "routines: %s/%s at %s -> local\n", cfg.Name, m.ID, m.At)
			}
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
