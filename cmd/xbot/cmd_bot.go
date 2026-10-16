package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/person"
	"xbot/internal/vault"
)

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
	store, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		return err
	}
	f, err := os.OpenFile(opt.Out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := bot.ExportTo(f, b, bot.TranscriptExport{
		Format: opt.Format, Tools: opt.Tools, From: opt.From, To: opt.To, Loc: time.Local, Redact: store.Redact,
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
