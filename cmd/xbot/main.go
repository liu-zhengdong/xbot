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
	"sync"
	"time"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/clip"
	"xbot/internal/code"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/desktop"
	"xbot/internal/eval"
	"xbot/internal/exec"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/memory"
	"xbot/internal/node"
	"xbot/internal/nodesync"
	"xbot/internal/notify"
	"xbot/internal/person"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/serve"
	"xbot/internal/skill"
	"xbot/internal/tools"
	"xbot/internal/tunnel"
	"xbot/internal/web"
	"xbot/internal/work"
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
  xbot [--root DIR] mcp add <bot> <name> --command PATH [--arg A]... [--secret-env NAME]...
  xbot [--root DIR] mcp remove <bot> <name>
  xbot [--root DIR] skill list
  xbot [--root DIR] skill import <path>
  xbot [--root DIR] skill show <slug>
  xbot [--root DIR] skill disable <bot> <slug>
  xbot [--root DIR] skill enable <bot> <slug>
  xbot [--root DIR] chat [name] [--yes] [--max-steps N]
  xbot [--root DIR] pin <name>
  xbot [--root DIR] unpin <name>
  xbot [--root DIR] primary
  xbot [--root DIR] primary set <name>
  xbot [--root DIR] profile show <name>
  xbot [--root DIR] profile set <name> [--description TEXT] [--tone TEXT] [--length short|medium|long] [--emoji none|spare|often] [--language TEXT] [--quiet HH:MM-HH:MM] [--timezone ZONE] [--name NEW]
  xbot [--root DIR] profile avatar <name> (--file PATH | --clear)
  xbot [--root DIR] template export <name> --out FILE
  xbot [--root DIR] template import --file FILE [--name NAME]
  xbot [--root DIR] export <bot> [--format md|jsonl] [--tools] [--from SEQ --to SEQ] --out FILE
  xbot [--root DIR] reindex <bot>
  xbot [--root DIR] bind <name> --provider NAME --model NAME [--uncertified]
  xbot [--root DIR] bind <name> --clear
  xbot [--root DIR] eval <provider/model> [--suite NAME|DIR] [--runs 3] [--out DIR]
  xbot [--root DIR] serve [--interval 15s] [--http 127.0.0.1:8787] [--tunnel cloudflare|tailscale]
  xbot [--root DIR] runner [--addr 127.0.0.1:18771] [--token TOKEN]
  xbot [--root DIR] webhook add <bot> <name> [--auth hmac|bearer] [--public-url URL]
  xbot [--root DIR] webhook list <bot>
  xbot [--root DIR] webhook remove <bot> <name>
  xbot [--root DIR] watch add <bot> <id> --path PATTERN
  xbot [--root DIR] watch list <bot>
  xbot [--root DIR] watch remove <bot> <id>
  xbot [--root DIR] node serve [--addr 127.0.0.1:18772] [--token TOKEN|-] [--name NAME]
  xbot [--root DIR] node add <name> --url URL [--token TOKEN|-]
  xbot [--root DIR] node list
  xbot [--root DIR] node remove <name>
  xbot [--root DIR] node call <name> info
  xbot [--root DIR] node call <name> shell <command>
  xbot [--root DIR] node approve <id>
  xbot [--root DIR] sync export --out FILE
  xbot [--root DIR] sync merge --file FILE
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
  xbot [--root DIR] provider add <name> --base-url URL --api-key-env VAR --model NAME --context N
  xbot [--root DIR] provider list [--all]
  xbot [--root DIR] provider remove <name> [--model NAME]

Environment:
  XBOT_API_KEY    API key when a bot has no provider binding
  XBOT_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com/v1)
  XBOT_MODEL       Model name (default deepseek-chat)
  XBOT_PUBLIC_URL   Base URL printed for new webhooks (default http://127.0.0.1:8787)
  XBOT_CLOUDFLARED  cloudflared binary for --tunnel cloudflare (default cloudflared)
  XBOT_TAILSCALE    tailscale binary for --tunnel tailscale (default tailscale)

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
	root, err = resolveRoot(root)
	if err != nil {
		return err
	}
	keyNames := config.KeyEnvNames(root)
	config.HoldKeys(keyNames)
	tools.UseKeyEnv(keyNames)
	tools.SetKeyLookup(config.Held)
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
	case "export":
		return cmdExport(root, rest[1:])
	case "reindex":
		return cmdReindex(root, rest[1:])
	case "bind":
		return cmdBind(root, rest[1:])
	case "eval":
		return cmdEval(root, rest[1:])
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
		return cmdNode(root, rest[1:])
	case "sync":
		return cmdSync(root, rest[1:])
	case "machine":
		return cmdMachine(root, rest[1:])
	case "desktop":
		return cmdDesktop(root, rest[1:])
	case "browser":
		return cmdBrowser(root, rest[1:])
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
	existing, err := bot.List(root)
	if err != nil {
		return err
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
	if len(existing) == 0 {
		if err := bot.SetPrimary(root, b.Name()); err != nil {
			return err
		}
	} else if _, err := bot.Primary(root); errors.Is(err, bot.ErrNoPrimary) || errors.Is(err, bot.ErrNotFound) {
		if _, _, err := bot.EnsurePrimary(root, bot.Seed{Persona: bot.DefaultPersona, Links: defaultLinks, Owner: who.ID}); err != nil {
			return err
		}
	}
	fmt.Printf("created bot %s\n", opt.Name)
	return nil
}

func ensureSeed(root string) error {
	ext, display, err := link.Account()
	if err != nil {
		return err
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		return err
	}
	b, created, err := bot.EnsurePrimary(root, bot.Seed{Persona: bot.DefaultPersona, Links: defaultLinks, Owner: who.ID})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	return b.WriteRules(kernel.DefaultRules())
}

type forkOpts struct {
	Src   string
	Name  string
	Role  string
	Group string
}

func parseFork(args []string) (forkOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"role": true, "group": true})
	if err != nil {
		return forkOpts{}, err
	}
	fs := flag.NewFlagSet("fork", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	role := fs.String("role", "", "persona and handoff focus for the new bot")
	group := fs.String("group", "", "optional group name containing both bots")
	if err := fs.Parse(ordered); err != nil {
		return forkOpts{}, err
	}
	if fs.NArg() != 2 || strings.TrimSpace(*role) == "" {
		return forkOpts{}, errors.New("usage: xbot fork <src> <name> --role TEXT [--group NAME]")
	}
	return forkOpts{Src: fs.Arg(0), Name: fs.Arg(1), Role: *role, Group: *group}, nil
}

func cmdFork(root string, args []string) error {
	opt, err := parseFork(args)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, opt.Src)
	if err != nil {
		return err
	}
	lock, ok, err := b.TryLockHandle()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("bot is busy")
	}
	defer lock.Release()
	k, _, err := openKernel(root, b, 4, func(context.Context, string, string) (bool, error) {
		return false, nil
	}, nil, false, runnerSpec{})
	if err != nil {
		return err
	}
	res, err := k.Fork(context.Background(), kernel.ForkRequest{Name: opt.Name, Role: opt.Role, Group: opt.Group})
	if err != nil {
		return err
	}
	fmt.Printf("forked %s from %s\n", res.Name, opt.Src)
	if res.GroupID != "" {
		fmt.Printf("group %s\t%s\n", res.GroupID, opt.Group)
	}
	return nil
}

func cmdList(root string) error {
	if err := ensureSeed(root); err != nil {
		return err
	}
	bots, err := bot.List(root)
	if err != nil {
		return err
	}
	bots = bot.Order(bots)
	primary, _ := bot.Primary(root)
	if len(bots) == 0 {
		fmt.Println("no bots")
		return nil
	}
	for _, cfg := range bots {
		extra := ""
		if cfg.Provider != "" {
			extra = "\t" + cfg.Provider + "/" + cfg.Model
		}
		var tags []string
		if cfg.Name == primary {
			tags = append(tags, "primary")
		}
		if cfg.Pinned {
			tags = append(tags, "pinned")
		}
		tag := ""
		if len(tags) > 0 {
			tag = "\t" + strings.Join(tags, ",")
		}
		fmt.Printf("%s\t%s%s%s\n", cfg.Name, strings.Join(cfg.Links, ","), tag, extra)
	}
	return nil
}

func cmdDelete(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot delete <name>")
	}
	if err := bot.Delete(root, args[0]); err != nil {
		return err
	}
	fmt.Printf("deleted %s\n", args[0])
	return nil
}

func cmdPin(root string, args []string, on bool) error {
	usage := "usage: xbot pin <name>"
	if !on {
		usage = "usage: xbot unpin <name>"
	}
	if len(args) != 1 {
		return errors.New(usage)
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := b.SetPinned(on); err != nil {
		return err
	}
	if on {
		fmt.Printf("pinned %s\n", b.Name())
	} else {
		fmt.Printf("unpinned %s\n", b.Name())
	}
	return nil
}

func cmdPrimary(root string, args []string) error {
	if len(args) == 0 {
		if err := ensureSeed(root); err != nil {
			return err
		}
		name, err := bot.Primary(root)
		if err != nil {
			return err
		}
		fmt.Println(name)
		return nil
	}
	if len(args) != 2 || args[0] != "set" {
		return errors.New("usage: xbot primary set <name>")
	}
	if err := bot.SetPrimary(root, args[1]); err != nil {
		return err
	}
	fmt.Printf("primary is %s\n", args[1])
	return nil
}

func cmdProfile(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot profile show|set|avatar")
	}
	switch args[0] {
	case "show":
		return cmdProfileShow(root, args[1:])
	case "set":
		return cmdProfileSet(root, args[1:])
	case "avatar":
		return cmdProfileAvatar(root, args[1:])
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func cmdProfileShow(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot profile show <name>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	st := b.StyleCopy()
	persona, err := b.ReadPersona()
	if err != nil {
		return err
	}
	quiet := ""
	if b.Config().QuietStart != "" || b.Config().QuietEnd != "" {
		quiet = b.Config().QuietStart + "-" + b.Config().QuietEnd
	}
	avatar := "color"
	if b.Config().Avatar == bot.AvatarCustom {
		avatar = "custom"
	}
	fmt.Printf("name: %s\n", b.Name())
	fmt.Printf("description: %s\n", b.Config().Description)
	fmt.Printf("tone: %s\n", st.Tone)
	fmt.Printf("length: %s\n", st.Length)
	fmt.Printf("emoji: %s\n", st.Emoji)
	fmt.Printf("language: %s\n", st.Language)
	fmt.Printf("quiet: %s\n", quiet)
	fmt.Printf("timezone: %s\n", b.Config().Timezone)
	fmt.Printf("group: %s\n", b.Config().Group)
	fmt.Printf("avatar: %s\n", avatar)
	fmt.Printf("persona: %s\n", persona)
	return nil
}

type profileSetOpts struct {
	Bot         string
	Description string
	Tone        string
	Length      string
	Emoji       string
	Language    string
	Quiet       string
	Timezone    string
	Name        string
	Set         map[string]bool
}

func parseProfileSet(args []string) (profileSetOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{
		"description": true, "tone": true, "length": true, "emoji": true,
		"language": true, "quiet": true, "timezone": true, "name": true,
	})
	if err != nil {
		return profileSetOpts{}, err
	}
	fs := flag.NewFlagSet("profile set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	desc := fs.String("description", "", "one-line description")
	tone := fs.String("tone", "", "speaking tone")
	length := fs.String("length", "", "short, medium, or long")
	emoji := fs.String("emoji", "", "none, spare, or often")
	lang := fs.String("language", "", "default reply language")
	quiet := fs.String("quiet", "", "HH:MM-HH:MM, or off")
	tz := fs.String("timezone", "", "IANA timezone, such as Asia/Shanghai")
	name := fs.String("name", "", "new bot name")
	if err := fs.Parse(ordered); err != nil {
		return profileSetOpts{}, err
	}
	if fs.NArg() != 1 {
		return profileSetOpts{}, errors.New("usage: xbot profile set <name> [flags]")
	}
	opt := profileSetOpts{
		Bot: fs.Arg(0), Description: *desc, Tone: *tone, Length: *length, Emoji: *emoji,
		Language: *lang, Quiet: *quiet, Timezone: *tz, Name: *name, Set: map[string]bool{},
	}
	fs.Visit(func(f *flag.Flag) { opt.Set[f.Name] = true })
	if len(opt.Set) == 0 {
		return profileSetOpts{}, errors.New("profile set needs at least one flag")
	}
	return opt, nil
}

func cmdProfileSet(root string, args []string) error {
	opt, err := parseProfileSet(args)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, opt.Bot)
	if err != nil {
		return err
	}
	var u bot.ProfileUpdate
	if opt.Set["description"] {
		s := opt.Description
		u.Description = &s
	}
	if opt.Set["tone"] {
		s := opt.Tone
		u.Tone = &s
	}
	if opt.Set["length"] {
		s := opt.Length
		u.Length = &s
	}
	if opt.Set["emoji"] {
		s := opt.Emoji
		u.Emoji = &s
	}
	if opt.Set["language"] {
		s := opt.Language
		u.Language = &s
	}
	if opt.Set["quiet"] {
		start, end, err := splitQuiet(opt.Quiet)
		if err != nil {
			return err
		}
		u.QuietStart = &start
		u.QuietEnd = &end
	}
	if opt.Set["timezone"] {
		s := opt.Timezone
		u.Timezone = &s
	}
	if u != (bot.ProfileUpdate{}) {
		if err := b.ApplyProfile(u); err != nil {
			return err
		}
	}
	name := b.Name()
	if opt.Set["name"] && opt.Name != b.Name() {
		if err := kernel.RenameBot(root, b.Name(), opt.Name); err != nil {
			return err
		}
		name = opt.Name
	}
	fmt.Printf("updated %s\n", name)
	return nil
}

func splitQuiet(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "off" || s == "-" {
		return "", "", nil
	}
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	start, end, ok := strings.Cut(s, "-")
	if !ok {
		return "", "", errors.New("quiet hours look like 22:00-07:00")
	}
	return strings.TrimSpace(start), strings.TrimSpace(end), nil
}

type avatarOpts struct {
	Bot   string
	File  string
	Clear bool
}

func parseProfileAvatar(args []string) (avatarOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"file": true})
	if err != nil {
		return avatarOpts{}, err
	}
	fs := flag.NewFlagSet("profile avatar", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "image file")
	clear := fs.Bool("clear", false, "remove the uploaded avatar")
	if err := fs.Parse(ordered); err != nil {
		return avatarOpts{}, err
	}
	if fs.NArg() != 1 {
		return avatarOpts{}, errors.New("usage: xbot profile avatar <name> (--file PATH | --clear)")
	}
	if *clear == (*file != "") {
		return avatarOpts{}, errors.New("pass one of --file or --clear")
	}
	return avatarOpts{Bot: fs.Arg(0), File: *file, Clear: *clear}, nil
}

func cmdProfileAvatar(root string, args []string) error {
	opt, err := parseProfileAvatar(args)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, opt.Bot)
	if err != nil {
		return err
	}
	if opt.Clear {
		if err := b.ClearAvatar(); err != nil {
			return err
		}
		fmt.Printf("cleared avatar for %s\n", b.Name())
		return nil
	}
	data, err := os.ReadFile(opt.File)
	if err != nil {
		return err
	}
	if err := b.SetAvatar(data, ""); err != nil {
		return err
	}
	fmt.Printf("avatar set for %s\n", b.Name())
	return nil
}

type exportOpts struct {
	Bot    string
	Format string
	Tools  bool
	From   int
	To     int
	Out    string
	Set    map[string]bool
}

func parseExport(args []string) (exportOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{
		"format": true, "from": true, "to": true, "out": true,
	})
	if err != nil {
		return exportOpts{}, err
	}
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "md", "md or jsonl")
	tools := fs.Bool("tools", false, "include tool calls and tool results")
	from := fs.Int("from", 0, "first sequence number")
	to := fs.Int("to", 0, "last sequence number")
	out := fs.String("out", "", "output file")
	if err := fs.Parse(ordered); err != nil {
		return exportOpts{}, err
	}
	if fs.NArg() != 1 || strings.TrimSpace(*out) == "" {
		return exportOpts{}, errors.New("usage: xbot export <bot> [--format md|jsonl] [--tools] [--from SEQ --to SEQ] --out FILE")
	}
	opt := exportOpts{Bot: fs.Arg(0), Format: *format, Tools: *tools, From: *from, To: *to, Out: *out, Set: map[string]bool{}}
	fs.Visit(func(f *flag.Flag) { opt.Set[f.Name] = true })
	if opt.Set["from"] != opt.Set["to"] {
		return exportOpts{}, errors.New("export: from and to are set together")
	}
	return opt, nil
}

func cmdExport(root string, args []string) error {
	opt, err := parseExport(args)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, opt.Bot)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(opt.Out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := bot.ExportTo(f, b, bot.TranscriptExport{
		Format: opt.Format, Tools: opt.Tools, From: opt.From, To: opt.To, Loc: time.Local,
	}); err != nil {
		return err
	}
	return f.Sync()
}

func cmdReindex(root string, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return errors.New("usage: xbot reindex <bot>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := b.Reindex(); err != nil {
		return err
	}
	fmt.Printf("reindexed %s\n", b.Name())
	return nil
}

func cmdTemplate(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot template export|import")
	}
	switch args[0] {
	case "export":
		return cmdTemplateExport(root, args[1:])
	case "import":
		return cmdTemplateImport(root, args[1:])
	default:
		return fmt.Errorf("unknown template command %q", args[0])
	}
}

type templateExportOpts struct {
	Name string
	Out  string
}

func parseTemplateExport(args []string) (templateExportOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"out": true})
	if err != nil {
		return templateExportOpts{}, err
	}
	fs := flag.NewFlagSet("template export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "output JSON file")
	if err := fs.Parse(ordered); err != nil {
		return templateExportOpts{}, err
	}
	if fs.NArg() != 1 || *out == "" {
		return templateExportOpts{}, errors.New("usage: xbot template export <name> --out FILE")
	}
	return templateExportOpts{Name: fs.Arg(0), Out: *out}, nil
}

func cmdTemplateExport(root string, args []string) error {
	opt, err := parseTemplateExport(args)
	if err != nil {
		return err
	}
	b, err := bot.Load(root, opt.Name)
	if err != nil {
		return err
	}
	tpl, err := bot.ExportTemplate(b)
	if err != nil {
		return err
	}
	if err := bot.WriteTemplate(opt.Out, tpl); err != nil {
		return err
	}
	fmt.Printf("exported %s\n", opt.Out)
	return nil
}

type templateImportOpts struct {
	File string
	Name string
}

func parseTemplateImport(args []string) (templateImportOpts, error) {
	ordered, err := reorderFlags(args, map[string]bool{"file": true, "name": true})
	if err != nil {
		return templateImportOpts{}, err
	}
	fs := flag.NewFlagSet("template import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "template JSON file")
	name := fs.String("name", "", "override the bot name")
	if err := fs.Parse(ordered); err != nil {
		return templateImportOpts{}, err
	}
	if fs.NArg() != 0 || *file == "" {
		return templateImportOpts{}, errors.New("usage: xbot template import --file FILE [--name NAME]")
	}
	return templateImportOpts{File: *file, Name: *name}, nil
}

func cmdTemplateImport(root string, args []string) error {
	opt, err := parseTemplateImport(args)
	if err != nil {
		return err
	}
	tpl, err := bot.ReadTemplate(opt.File)
	if err != nil {
		return err
	}
	if opt.Name != "" {
		tpl.Name = opt.Name
	}
	existing, err := bot.List(root)
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
	b, err := bot.ImportTemplate(root, tpl, defaultLinks, who.ID)
	if err != nil {
		return err
	}
	if err := b.WriteRules(kernel.DefaultRules()); err != nil {
		return err
	}
	if len(existing) == 0 {
		if err := bot.SetPrimary(root, b.Name()); err != nil {
			return err
		}
	} else if _, err := bot.Primary(root); errors.Is(err, bot.ErrNoPrimary) || errors.Is(err, bot.ErrNotFound) {
		if _, _, err := bot.EnsurePrimary(root, bot.Seed{Links: defaultLinks, Owner: who.ID}); err != nil {
			return err
		}
	}
	fmt.Printf("imported %s\n", b.Name())
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
	if fs.NArg() > 1 {
		return chatOpts{}, errors.New("usage: xbot chat [name]")
	}
	name := ""
	if fs.NArg() == 1 {
		name = fs.Arg(0)
	}
	return chatOpts{Name: name, Yes: *yes, MaxSteps: *steps, RunnerURL: *runnerURL, RunnerToken: *runnerToken}, nil
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
	k, who, err := openKernel(root, b, opt.MaxSteps, pr.Approve, pr, opt.Yes, runnerSpec{opt.RunnerURL, opt.RunnerToken})
	if err != nil {
		return err
	}
	k.StartEmitters(ctx)
	seen, err := b.Count()
	if err != nil {
		return err
	}
	go feedLines(bufio.NewReader(os.Stdin), lines)
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
		CatchUp: func() bool {
			return showTranscriptSince(b, &seen)
		},
		After: func(_ link.Event, err error) {
			pr.Break()
			if err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "xbot:", err)
			}
			if n, err := b.Count(); err == nil {
				seen = n
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
		if cmd := strings.TrimSpace(os.Getenv("XBOT_CODE_AGENT")); cmd != "" {
			coding.UseCommand(cmd)
		}
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
	defer func() { _ = browser.Stop(root) }()
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

// showTranscriptSince prints user and assistant lines appended by another
// process, such as a web turn that ran while this chat sat at the prompt.
func showTranscriptSince(b *bot.Bot, seen *int) bool {
	if b == nil || seen == nil {
		return false
	}
	entries, err := b.Entries()
	if err != nil || len(entries) <= *seen {
		return false
	}
	fresh := entries[*seen:]
	*seen = len(entries)
	printed := false
	for _, e := range fresh {
		text := strings.TrimSpace(e.Message.Content)
		if text == "" || strings.Contains(text, "<environment_") || bot.SilentText(text) {
			continue
		}
		switch e.Message.Role {
		case provider.RoleUser, provider.RoleAssistant:
			if len(e.Message.ToolCalls) > 0 {
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
