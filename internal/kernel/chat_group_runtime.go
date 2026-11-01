package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/peer"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// ChatGroupHandlers keeps the original listener's source identity and resources.
// Only the short source snapshot and artifact IO hold the current storage route;
// group reads and Bus fanout run after that route has been released.
func (k *Kernel) ChatGroupHandlers() *cliops.ChatGroupHandlers {
	source, resources := k.Bot, k.Resources
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
		return errors.New(redact(err.Error()))
	}
	currentIO := func(ctx context.Context, fn func(*bot.Bot, *inbox.Inbox) error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if resources != nil {
			return resources.WithCurrentIO(fn)
		}
		if source == nil {
			return errors.New("chat-group: no source bot")
		}
		return fn(source, nil)
	}
	snapshotSourceNameRoot := func(ctx context.Context) (name, root string, err error) {
		err = currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
			name, root = current.Name(), current.Root
			return nil
		})
		return
	}
	groupView := func(g peer.Group) cliops.ChatGroup {
		members := make([]string, 0, len(g.Members))
		for _, member := range g.Members {
			members = append(members, redact(member))
		}
		return cliops.ChatGroup{ID: redact(g.ID), Name: redact(g.Name), Members: members, Created: g.Created}
	}
	fits := func(value any, human string) bool {
		raw, err := json.Marshal(value)
		return err == nil && len(raw)+1 <= tools.DefaultShellOutputBytes && len(human) <= tools.DefaultShellOutputBytes
	}
	// Both real read callers project the complete redacted JSON, including its
	// newline. Reserve WriteResult's filename before creating a local artifact.
	project := func(ctx context.Context, value any, human string, reference func(string, int64) (any, string)) (string, int64, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			return "", 0, err
		}
		raw = append(raw, '\n')
		if len(raw) <= tools.DefaultShellOutputBytes && len(human) <= tools.DefaultShellOutputBytes {
			return "", 0, nil
		}
		if len(raw) > work.ResultMaxBytes {
			return "", 0, fmt.Errorf("chat-group: result is %d bytes, over %d", len(raw), work.ResultMaxBytes)
		}
		placeholder := work.ResultDir + "/" + strings.Repeat("0", 32) + ".json"
		metadata, text := reference(placeholder, int64(len(raw)))
		if !fits(metadata, text) {
			return "", 0, errors.New("chat-group: metadata exceeds response budget")
		}
		var file string
		err = currentIO(ctx, func(current *bot.Bot, _ *inbox.Inbox) error {
			var err error
			file, err = work.WriteResult(work.BotDir(current.Root, current.Name()), raw, "json")
			return err
		})
		return file, int64(len(raw)), err
	}
	return &cliops.ChatGroupHandlers{
		List: func(ctx context.Context) (cliops.ChatGroupListReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.ChatGroupListReply, error) {
				err = failure(err)
				return cliops.ChatGroupListReply{Error: err.Error()}, err
			}
			name, root, err := snapshotSourceNameRoot(ctx)
			if err != nil {
				return fail(err)
			}
			groups, err := (&peer.Bus{Root: root}).ListGroups()
			if err != nil {
				return fail(err)
			}
			views := make([]cliops.ChatGroup, 0, len(groups))
			for _, group := range groups {
				if slices.Contains(group.Members, name) {
					views = append(views, groupView(group))
				}
			}
			reply := cliops.ChatGroupListReply{Groups: &views, Count: len(views)}
			file, size, err := project(ctx, reply, cliops.FormatChatGroupList(reply), func(file string, size int64) (any, string) {
				ref := cliops.ChatGroupListReply{Count: reply.Count, File: file, Bytes: size, Access: "local"}
				return ref, cliops.FormatChatGroupList(ref)
			})
			if err != nil {
				return fail(err)
			}
			if file != "" {
				reply.Groups, reply.File, reply.Bytes, reply.Access = nil, file, size, "local"
			}
			return reply, nil
		},
		History: func(ctx context.Context, query cliops.ChatGroupHistory) (cliops.ChatGroupHistoryReply, error) {
			release := k.holdOperations()
			defer release()
			fail := func(err error) (cliops.ChatGroupHistoryReply, error) {
				err = failure(err)
				return cliops.ChatGroupHistoryReply{Untrusted: true, Error: err.Error()}, err
			}
			name, root, err := snapshotSourceNameRoot(ctx)
			if err != nil {
				return fail(err)
			}
			bus := &peer.Bus{Root: root}
			group, err := bus.LoadGroup(query.ID)
			if err != nil {
				return fail(err)
			}
			if !slices.Contains(group.Members, name) {
				return fail(fmt.Errorf("you are not a member of group %s", query.ID))
			}
			lines, err := bus.Transcript(query.ID)
			if err != nil {
				return fail(err)
			}
			if query.BeforeID != "" {
				cut := slices.IndexFunc(lines, func(line peer.Line) bool { return line.ID == query.BeforeID })
				if cut < 0 {
					return fail(fmt.Errorf("no message %s in group %s", query.BeforeID, query.ID))
				}
				lines = lines[:cut]
			}
			limit := query.Limit
			if limit <= 0 {
				limit = 20
			}
			limit = min(limit, 50)
			messages := make([]cliops.ChatGroupLine, 0, min(len(lines), limit))
			for i := len(lines) - 1; i >= 0 && len(messages) < limit; i-- {
				line := lines[i]
				if query.Sender != "" && line.Sender != query.Sender {
					continue
				}
				if query.Keyword != "" && !strings.Contains(strings.ToLower(line.Text), strings.ToLower(query.Keyword)) {
					continue
				}
				messages = append(messages, cliops.ChatGroupLine{ID: redact(line.ID), Time: line.Time, Sender: redact(line.Sender), Kind: redact(line.Kind), Text: redact(line.Text)})
			}
			slices.Reverse(messages)
			view := groupView(group)
			reply := cliops.ChatGroupHistoryReply{Group: &view, Messages: &messages, Untrusted: true, Count: len(messages)}
			file, size, err := project(ctx, reply, cliops.FormatChatGroupHistory(reply), func(file string, size int64) (any, string) {
				ref := cliops.ChatGroupHistoryReply{Group: &view, Untrusted: true, Count: reply.Count, File: file, Bytes: size, Access: "local"}
				return ref, cliops.FormatChatGroupHistory(ref)
			})
			if err != nil {
				return fail(err)
			}
			if file != "" {
				reply.Messages, reply.File, reply.Bytes, reply.Access = nil, file, size, "local"
			}
			return reply, nil
		},
		Create: func(ctx context.Context, in cliops.ChatGroupCreate) (cliops.ChatGroupCreateReply, error) {
			release := k.holdOperations()
			defer release()
			reply := cliops.ChatGroupCreateReply{}
			name, root, err := snapshotSourceNameRoot(ctx)
			if err == nil {
				var group peer.Group
				group, err = (&peer.Bus{Root: root, Now: k.now}).CreateGroup(name, in.Name, in.Members)
				if err == nil {
					view := groupView(group)
					reply.Group, reply.Created = &view, true
				}
			}
			if err != nil {
				err = failure(err)
				reply.Error = err.Error()
			}
			if !fits(reply, cliops.FormatChatGroupCreate(reply)) {
				err = errors.New("chat-group: receipt exceeds response budget")
				reply.Group, reply.Error = nil, err.Error()
			}
			return reply, err
		},
		Post: func(ctx context.Context, in cliops.ChatGroupPost) (cliops.ChatGroupPostReply, error) {
			release := k.holdOperations()
			defer release()
			// The decoded body is fixed before resolving the original Shell call.
			id, text := in.ID, in.Text
			result := peer.GroupPostResult{GroupID: id, PostedMembers: []string{}, Stage: "validation"}
			prepared, cleanup, err := k.runtimeSourceCall(ctx)
			if err == nil {
				defer cleanup()
				frozen, ok := prepared.Value(turnKey{}).(*turn)
				if !ok || frozen == nil {
					err = errors.New("chat-group: source frozen turn unavailable")
				} else {
					frozen.mu.Lock()
					chain, hops := frozen.turnChain, frozen.turnHops
					frozen.mu.Unlock()
					var name, root string
					name, root, err = snapshotSourceNameRoot(prepared)
					if err == nil {
						if chain == "" {
							chain = "ch_" + name
						}
						result, err = (&peer.Bus{Root: root, Now: k.now}).PostGroupWithResult(link.KindBot, name, id, text, chain, hops+1, k.now())
					}
				}
			}
			posted := make([]string, 0, len(result.PostedMembers))
			for _, member := range result.PostedMembers {
				posted = append(posted, redact(member))
			}
			reply := cliops.ChatGroupPostReply{ID: redact(result.GroupID), MessageID: redact(result.MessageID), Appended: result.Appended, PostedMembers: posted, FailedMember: redact(result.FailedMember), Stage: result.Stage}
			if err != nil {
				err = failure(err)
				reply.Error = err.Error()
			}
			if !fits(reply, cliops.FormatChatGroupPost(reply)) {
				err = errors.New("chat-group: receipt exceeds response budget")
				reply.Error = err.Error()
			}
			return reply, err
		},
	}
}
