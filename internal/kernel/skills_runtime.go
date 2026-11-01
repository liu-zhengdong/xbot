package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/config"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/skill"
	"xbot/internal/work"
)

const skillRuntimePreview = 2 << 10

// SkillHandlers captures this source's configuration and resource identity once.
// Later kernels sharing the resource cannot replace its library or redactor.
func (k *Kernel) SkillHandlers() *cliops.SkillHandlers {
	root, dir := "", k.SkillsDir
	if k.Bot != nil {
		root = k.Bot.Root
	}
	resources := k.Resources
	baseRedact := k.Redact
	if baseRedact == nil {
		baseRedact = bot.RedactText
	}
	redact := func(text string) string {
		text = config.RedactHeld(baseRedact(text))
		if resources != nil && resources.Redact != nil {
			text = resources.Redact(text)
		}
		return text
	}
	failure := func(err error) error {
		if err == nil {
			return nil
		}
		return errors.New(redact(err.Error()))
	}
	currentIO := func(fn func(*bot.Bot, *inbox.Inbox) error) error {
		if resources != nil {
			return resources.WithCurrentIO(fn)
		}
		if k.Bot == nil {
			return fmt.Errorf("skill: no bot")
		}
		return fn(k.Bot, k.Inbox)
	}
	return &cliops.SkillHandlers{
		Read: func(ctx context.Context, id string) (cliops.SkillReadReply, error) {
			release := k.holdOperations()
			defer release()
			if err := ctx.Err(); err != nil {
				return cliops.SkillReadReply{}, failure(err)
			}
			var disabled []string
			if err := currentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
				disabled = current.DisabledSkills()
				return nil
			}); err != nil {
				return cliops.SkillReadReply{}, failure(err)
			}
			info, body, err := readRuntimeSkill(dir, id, disabled)
			if err != nil {
				return cliops.SkillReadReply{}, failure(err)
			}
			body = redact(body)
			reply := cliops.SkillReadReply{
				Slug: redact(info.Slug), Name: redact(info.Name), Title: redact(info.Title),
				Description: redact(info.Description), Body: &body, Bytes: int64(len(body)),
			}
			if skillReadReplyFits(reply) {
				return reply, nil
			}
			reply.Body = nil
			reply.Access = "local"
			// Reserve the exact length of WriteResult's random local filename before
			// writing an artifact; metadata that cannot fit is a projection error.
			reply.File = work.ResultDir + "/" + strings.Repeat("0", 32) + ".txt"
			if !skillReadReplyFits(reply) {
				err := failure(fmt.Errorf("skill: metadata exceeds response budget"))
				return cliops.SkillReadReply{Error: err.Error()}, err
			}
			for limit := skillRuntimePreview; limit > 0; limit /= 2 {
				reply.Preview, _ = clip.Bytes(body, limit)
				if skillReadReplyFits(reply) {
					break
				}
				reply.Preview = ""
			}
			err = currentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
				var err error
				reply.File, err = work.WriteResult(work.BotDir(current.Root, current.Name()), []byte(body), "txt")
				return err
			})
			if err != nil {
				err = failure(err)
				return cliops.SkillReadReply{Error: err.Error()}, err
			}
			return reply, nil
		},
		Write: func(ctx context.Context, in cliops.SkillWrite) (cliops.SkillWriteReply, error) {
			release := k.holdOperations()
			defer release()
			if err := ctx.Err(); err != nil {
				return cliops.SkillWriteReply{}, failure(err)
			}
			var reply cliops.SkillWriteReply
			err := currentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
				var err error
				reply, err = writeRuntimeSkill(root, dir, current.Name(), in)
				return err
			})
			if err != nil {
				err = failure(err)
				reply.Error = err.Error()
			}
			return reply, err
		},
	}
}

func skillReadReplyFits(reply cliops.SkillReadReply) bool {
	raw, _ := json.Marshal(reply)
	return len(raw)+1 <= cliops.SkillMaxReply
}

func readRuntimeSkill(dir, id string, disabled []string) (skill.Info, string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return skill.Info{}, "", fmt.Errorf("skill: id is required")
	}
	if dir == "" {
		return skill.Info{}, "", fmt.Errorf("skill: skills directory is not configured")
	}
	infos, err := skill.ListBounded(dir, work.ResultMaxBytes)
	if err != nil {
		return skill.Info{}, "", err
	}
	info, err := resolveSkillInfo(infos, id, disabled)
	if err != nil {
		return skill.Info{}, "", err
	}
	return skill.OpenBounded(dir, info.Slug, work.ResultMaxBytes)
}

// resolveSkillInfo is shared with routine/legacy reads. Identity is the first
// slug or legacy name in library order; a title is never an identifier.
func resolveSkillInfo(infos []skill.Info, id string, disabled []string) (skill.Info, error) {
	for _, info := range infos {
		if info.Slug != id && info.Name != id {
			continue
		}
		for _, slug := range disabled {
			if strings.TrimSpace(slug) == info.Slug {
				return skill.Info{}, fmt.Errorf("skill: disabled")
			}
		}
		return info, nil
	}
	return skill.Info{}, fmt.Errorf("skill: %s not found", id)
}

// resolveSkillScope only reads configs. List/Load can migrate legacy memory,
// which must not occur during scope preflight or inside the source route.
func resolveSkillScope(root, scope, currentName string) ([]string, string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "bots"))
	if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	scope = strings.TrimSpace(scope)
	if strings.EqualFold(scope, "self") {
		scope = currentName
	}
	var configuredNames []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, err := readSkillBotName(root, entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			// Match List's treatment of malformed/inaccessible local configs.
			log.Printf("bot: skip %q: %v", entry.Name(), err)
			continue
		}
		configuredNames = append(configuredNames, name)
	}
	sort.Strings(configuredNames)
	var names []string
	target := ""
	for _, configuredName := range configuredNames {
		// The original scope path loaded each listed config by cfg.Name again.
		// Keep that identity check without invoking Load's legacy migrations.
		name, err := readSkillBotName(root, configuredName)
		if err != nil {
			return nil, "", err
		}
		names = append(names, name)
		if scope != "" && strings.EqualFold(scope, configuredName) {
			if target != "" {
				return nil, "", fmt.Errorf("skill: ambiguous scope")
			}
			target = configuredName
		}
	}
	if scope != "" && target == "" {
		return nil, "", fmt.Errorf("skill: scope must name one of the local bots")
	}
	return names, target, nil
}

func readSkillBotName(root, name string) (string, error) {
	if !bot.ValidName(name) {
		return "", fmt.Errorf("bot: invalid name %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(bot.Dir(root, name), "config.json"))
	if err != nil {
		return "", err
	}
	var cfg bot.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", err
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	return cfg.Name, nil
}

func writeRuntimeSkill(root, configuredDir, currentName string, in cliops.SkillWrite) (cliops.SkillWriteReply, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Scope = strings.TrimSpace(in.Scope)
	in.Description = oneSkillLine(in.Description)
	reply := cliops.SkillWriteReply{Slug: in.Slug, Scope: in.Scope, UpdatedBots: []string{}}
	if root == "" || currentName == "" {
		return reply, fmt.Errorf("skill: no bot")
	}
	if configuredDir == "" {
		return reply, fmt.Errorf("skill: skills directory is not configured")
	}
	if !skill.ValidSlug(in.Slug) {
		return reply, fmt.Errorf("skill: bad slug %q", in.Slug)
	}
	if in.Description == "" {
		return reply, fmt.Errorf("skill: description is required")
	}
	if in.Body == nil {
		return reply, fmt.Errorf("skill: body is required")
	}
	if _, _, err := resolveSkillScope(root, in.Scope, currentName); err != nil {
		return reply, err
	}
	lock, err := fsutil.Lock(filepath.Join(skill.Dir(root), ".write.lock"))
	if err != nil {
		return reply, err
	}
	defer lock.Release()
	// A waiter must not apply the Bot set captured before it acquired the lock.
	names, target, err := resolveSkillScope(root, in.Scope, currentName)
	if err != nil {
		return reply, err
	}
	reply.Target = target
	if err := skill.Write(root, in.Slug, in.Description, *in.Body, in.Title); err != nil {
		return reply, err
	}
	reply.Written = true
	for _, name := range names {
		if err := bot.SetSkillDisabled(root, name, in.Slug, target != "" && name != target); err != nil {
			return reply, fmt.Errorf("skill: file written but scope update incomplete: %w", err)
		}
		reply.UpdatedBots = append(reply.UpdatedBots, name)
	}
	return reply, nil
}
