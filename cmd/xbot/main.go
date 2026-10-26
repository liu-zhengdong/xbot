package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/clip"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/proc"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

const usageText = `XBot is a personal bot. One bot has one continuous conversation.

Usage:
  xbot [--root DIR] new <name> [--persona TEXT] [--provider NAME] [--model NAME]
  xbot [--root DIR] fork <src> <name> --role TEXT [--group NAME]
  xbot [--root DIR] delete <name>
  xbot [--root DIR] list
  xbot [--root DIR] group list
  xbot [--root DIR] group create <name>
  xbot [--root DIR] group rename <old> <new>
  xbot [--root DIR] group delete <name>
  xbot [--root DIR] group set <bot> <name>
  xbot [--root DIR] group set <bot> --clear
  xbot [--root DIR] link list [bot]
  xbot [--root DIR] link install <bot> <id>
  xbot [--root DIR] link remove <bot> <id>
  xbot [--root DIR] mcp list <bot>
  xbot [--root DIR] mcp add <bot> <name> (--command PATH [--arg A]... | --url URL) [--secret-env NAME]...
  xbot [--root DIR] mcp remove <bot> <name>
  xbot [--root DIR] secret set <bot> NAME --domains a.com,b.com
  xbot [--root DIR] secret list <bot>
  xbot [--root DIR] secret rm <bot> NAME
  xbot [--root DIR] secret migrate <bot>
  xbot [--root DIR] vaultd --dir DIR --socket PATH --allow-uid N[,N]
  xbot [--root DIR] connector list
  xbot [--root DIR] connector add <bot> <id>
  xbot [--root DIR] connector remove <bot> <id>
  xbot [--root DIR] skill list
  xbot [--root DIR] skill import <path>
  xbot [--root DIR] skill show <slug>
  xbot [--root DIR] skill disable <bot> <slug>
  xbot [--root DIR] skill enable <bot> <slug>
  xbot [--root DIR] chat [name] [--yes]
  xbot [--root DIR] pin <name>
  xbot [--root DIR] unpin <name>
  xbot [--root DIR] primary
  xbot [--root DIR] primary set <name>
  xbot [--root DIR] profile show <name>
  xbot [--root DIR] profile set <name> [--description TEXT] [--timezone ZONE] [--role chat|dev] [--background-max-time MIN] [--name NEW]
  xbot [--root DIR] profile avatar <name> (--file PATH | --clear)
  xbot [--root DIR] template export <name> --out FILE
  xbot [--root DIR] template import --file FILE [--name NAME]
  xbot [--root DIR] routine migrate [--dry-run] [--bot NAME]
  xbot [--root DIR] export <bot> [--format md|jsonl] [--tools] [--from SEQ --to SEQ] --out FILE
  xbot [--root DIR] reindex <bot>
  xbot [--root DIR] bind <name> --provider NAME --model NAME [--uncertified]
  xbot [--root DIR] bind <name> --clear
  xbot [--root DIR] eval <provider/model> [--suite NAME|DIR] [--runs 3] [--out DIR]
  xbot eval scorecard <report.json>
  xbot [--root DIR] app [--port 8787]
  xbot [--root DIR] serve [--interval 15s] [--http 127.0.0.1:8787] [--tunnel cloudflare|tailscale] [--no-auto-upgrade]
  xbot [--root DIR] runner [--addr 127.0.0.1:18771] [--token TOKEN]
  xbot account-api   (env: DATA_DIR, BASE_URL, ADDR, MAIL_MODE, MAIL_OUTBOX)
  xbot [--root DIR] webhook add <bot> <name> [--auth hmac|bearer] [--public-url URL]
  xbot [--root DIR] webhook list <bot>
  xbot [--root DIR] webhook remove <bot> <name>
  xbot [--root DIR] watch add <bot> <id> --path PATTERN
  xbot [--root DIR] watch list <bot>
  xbot [--root DIR] watch remove <bot> <id>
  xbot [--root DIR] remote id
  xbot [--root DIR] remote gateway [URL|default]
  xbot [--root DIR] remote add <name> <key> [--xkey KEY] [--person ID]
  xbot [--root DIR] remote invite [--person ID]
  xbot [--root DIR] remote join <code> [--name NAME] [--person ID]
  xbot [--root DIR] remote list
  xbot [--root DIR] remote remove <name>
  xbot [--root DIR] gateway serve [--addr 127.0.0.1:8790] [--dir DIR] [--open]
  xbot pipe serve [--addr 127.0.0.1:8792] [--public URL]   (env ACCOUNT_API)
  xbot [--root DIR] person unlink [--force] <link:id>
  xbot [--root DIR] permissions [on|off]   (off by default: no confirmation cards)
  xbot [--root DIR] machine list
  xbot [--root DIR] machine set <name> allow|ask|off
  xbot [--root DIR] desktop screenshot
  xbot [--root DIR] desktop click <x> <y>
  xbot [--root DIR] desktop type <text>
  xbot [--root DIR] desktop scroll <dy>
  xbot [--root DIR] desktop key <key>
  xbot [--root DIR] browser open [--headless] <url>
  xbot [--root DIR] browser snapshot
  xbot [--root DIR] browser click <selector>
  xbot [--root DIR] browser stop
  xbot [--root DIR] provider add <name> --base-url URL --api-key-env VAR --model NAME --context N [--protocol chat_completions|responses|anthropic_messages] [--hosted-web-search on|off] [--todo on|off] [--todo-steps N] [--thinking on|off|omit] [--reasoning-effort low|high|max]
  xbot [--root DIR] provider list [--all]
  xbot [--root DIR] provider remove <name> [--model NAME]
  xbot version
  xbot [--root DIR] upgrade [--check]

Environment:
  XBOT_API_KEY    API key when a bot has no provider binding
  XBOT_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com/v1)
  XBOT_MODEL       Model name (default deepseek-flash)
  XBOT_PUBLIC_URL   Base URL printed for new webhooks (default http://127.0.0.1:8787)
  XBOT_CLOUDFLARED  cloudflared binary for --tunnel cloudflare (default cloudflared)
  XBOT_TAILSCALE    tailscale binary for --tunnel tailscale (default tailscale)
  XBOT_VAULT_SOCKET  vault daemon unix socket; when set, secrets use the daemon
  XBOT_AUTO_UPGRADE  0 disables the periodic release check in serve
  XBOT_UPGRADE_URL   override the release endpoint (default the GitHub API)
  XBOT_DEV_STATIC    development only: serve web files from this directory instead of the embedded copy

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
	root, rest, err := splitGlobal(launchArgs(argv))
	if errors.Is(err, errUsage) && defaultCommand != "" {
		err = nil // a double-clicked desktop build runs defaultCommand
	}
	if err != nil {
		return err
	}
	root, err = resolveRoot(root)
	if err != nil {
		return err
	}
	if len(rest) == 0 && defaultCommand != "" {
		rest = []string{defaultCommand}
	}
	if len(rest) > 0 && rest[0] == "app" {
		if err := loadEnvFile(filepath.Join(root, "env")); err != nil {
			return err
		}
	}
	keyNames := config.KeyEnvNames(root)
	config.HoldKeys(keyNames)
	if err := config.HoldSearchCredentials(root); err != nil {
		return fmt.Errorf("载入搜索密钥失败：%w", err)
	}
	link.SetProviderKey(config.ProviderKey)
	tools.UseKeyEnv(keyNames)
	tools.SetKeyLookup(config.Held)
	bot.SetRedactText(vault.Redact)
	tools.SetMachineGate(func() (string, error) {
		return machine.Get(root, machine.Local)
	})
	if len(rest) == 0 {
		return errUsage
	}
	switch rest[0] {
	case "new":
		return cmdNew(root, rest[1:])
	case "fork":
		return cmdFork(root, rest[1:])
	case "delete":
		return cmdDelete(root, rest[1:])
	case "list":
		return cmdList(root)
	case "group":
		return cmdGroup(root, rest[1:])
	case "link":
		return cmdLink(root, rest[1:])
	case "mcp":
		return cmdMCP(root, rest[1:])
	case "secret":
		return cmdSecret(root, rest[1:])
	case "vaultd":
		return cmdVaultd(root, rest[1:])
	case "connector":
		return cmdConnector(root, rest[1:])
	case "skill":
		return cmdSkill(root, rest[1:])
	case "chat":
		return cmdChat(root, rest[1:])
	case "pin":
		return cmdPin(root, rest[1:], true)
	case "unpin":
		return cmdPin(root, rest[1:], false)
	case "primary":
		return cmdPrimary(root, rest[1:])
	case "profile":
		return cmdProfile(root, rest[1:])
	case "template":
		return cmdTemplate(root, rest[1:])
	case "routine":
		return cmdRoutine(root, rest[1:])
	case "export":
		return cmdExport(root, rest[1:])
	case "reindex":
		return cmdReindex(root, rest[1:])
	case "bind":
		return cmdBind(root, rest[1:])
	case "eval":
		return cmdEval(root, rest[1:])
	case "app":
		err := cmdApp(root, rest[1:])
		if err != nil && defaultCommand != "" {
			logAppError(root, err)
		}
		return err
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
	case "node":
		return errors.New("xbot node 已移除：多台电脑之间请在「设备」里配对（命令行：xbot remote invite / xbot remote join）")
	case "remote":
		return cmdRemote(root, rest[1:])
	case "gateway":
		return cmdGateway(root, rest[1:])
	case "pipe":
		return cmdPipe(rest[1:])
	case "person":
		return cmdPerson(root, rest[1:])
	case "sync":
		return errors.New("xbot sync 已移除：每台电脑各自保留数据，不再同步人和记忆")
	case "version":
		return cmdVersion()
	case "upgrade":
		return cmdUpgrade(root, rest[1:])
	case "permissions":
		return cmdPermissions(root, rest[1:])
	case "machine":
		return cmdMachine(root, rest[1:])
	case "desktop":
		return cmdDesktop(root, rest[1:])
	case "browser":
		return cmdBrowser(root, rest[1:])
	case "account-api":
		return cmdAccountAPI(rest[1:])
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

// resolveRoot makes --root absolute before any bot directory is derived from it.
// A relative root otherwise makes subagent directories relative, and spawn rejects them.
func resolveRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		var err error
		root, err = defaultRoot()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(root)
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
	link.NameWeb, link.NamePeers,
}

type runnerSpec struct {
	URL   string
	Token string
}

type printer struct {
	yes     bool
	reader  *bufio.Reader
	lines   <-chan proc.Line
	out     io.Writer
	mu      sync.Mutex
	pending strings.Builder
	wrote   bool
	nl      bool
}

func (p *printer) stdout() io.Writer {
	if p.out != nil {
		return p.out
	}
	return os.Stdout
}

func (p *printer) Delta(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending.WriteString(s)
	if bot.SilentPrefix(p.pending.String()) {
		return
	}
	p.writePending()
}

func (p *printer) writePending() {
	s := p.pending.String()
	p.pending.Reset()
	if s == "" || bot.SilentText(s) {
		return
	}
	fmt.Fprint(p.stdout(), s)
	p.wrote = true
	p.nl = strings.HasSuffix(s, "\n")
}

func (p *printer) Break() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if bot.SilentText(p.pending.String()) {
		p.pending.Reset()
	} else {
		p.writePending()
	}
	p.breakLocked()
}

func (p *printer) breakLocked() {
	if p.wrote && !p.nl {
		fmt.Fprintln(os.Stdout)
	}
	p.wrote = false
}

func (p *printer) Tool(name, summary string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.breakLocked()
	if name == "ask_user" && strings.TrimSpace(summary) != "" {
		fmt.Fprintln(p.stdout(), summary)
		p.wrote = true
		p.nl = true
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] %s\n", name, summary)
}

func (p *printer) Result(_, output string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	shown, _ := clip.Bytes(output, 400)
	fmt.Fprintln(os.Stderr, shown)
}

func (p *printer) Approve(ctx context.Context, _, _ string) (bool, error) {
	if p.yes {
		return true, nil
	}
	p.mu.Lock()
	fmt.Fprint(os.Stderr, "Allow? [y/n] ")
	p.mu.Unlock()
	var line string
	var err error
	if p.lines != nil {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case got, ok := <-p.lines:
			if !ok {
				return false, io.EOF
			}
			line, err = got.Text, got.Err
		}
	} else {
		line, err = readLine(ctx, p.reader)
	}
	if err != nil && strings.TrimSpace(line) == "" {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
