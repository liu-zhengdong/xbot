package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func chatGroupRuntimeSource(t *testing.T, peers ...string) (*Kernel, peer.Group, string) {
	t.Helper()
	k, _, _ := freshBot(t)
	if err := k.Bot.SetOwners([]string{"shared"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range peers {
		chatGroupRuntimeBot(t, k.Bot.Root, name, "shared")
	}
	g := chatGroupRuntimeGroup(t, &peer.Bus{Root: k.Bot.Root, Now: k.now}, k.Bot.Name(), "room", peers...)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.CloseOperations(); _ = k.Close() })
	return k, g, operationEndpoint(k)
}

func chatGroupRuntimePost(t *testing.T, k *Kernel, endpoint string, in cliops.ChatGroupPost) (reply cliops.ChatGroupPostReply, postErr error) {
	t.Helper()
	runRoutineSource(t, k, nil, `{"command":"\"$XBOT_BIN\" chat-group post --self --file body.txt","description":"post agreed message"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, postErr = cliops.CallChatGroupPost(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), in)
		return "checked original source HTTP", nil
	})
	return
}

func TestChatGroupRuntimePostPreparedFrozenContextNoApprover(t *testing.T) {
	k, group, endpoint := chatGroupRuntimeSource(t, "B")
	owner := k.ensureTurn()
	owner.mu.Lock()
	owner.turnChain, owner.turnHops, owner.branch = "ch_original", 1, "bg_original"
	owner.mu.Unlock()
	var reply cliops.ChatGroupPostReply
	var postErr error
	var originalCall string
	runRoutineSource(t, k, nil, `{"command":"original source command","description":"post agreed message"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if _, approve := agent.ToolApprover(ctx); approve != nil {
			t.Error("fixture must have no approver")
		}
		originalCall = tools.CallID(ctx)
		if originalCall == "" {
			t.Error("real Job did not supply a CallID")
		}
		// Preparation already copied the tuple. Change both the original owner
		// and latest turn before making the source HTTP request.
		owner.mu.Lock()
		owner.turnChain, owner.turnHops = "ch_owner_changed", 3
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{turnChain: "ch_latest", turnHops: 3}
		k.turnMu.Unlock()
		reply, postErr = cliops.CallChatGroupPost(ctx, endpoint, k.Bot.Root, originalCall, cliops.ChatGroupPost{ID: group.ID, Text: "  full agreed body\n"})
		return "posted", nil
	})
	if postErr != nil || !reply.Appended || reply.MessageID == "" || reply.ID != group.ID || reply.Stage != "complete" || !reflect.DeepEqual(reply.PostedMembers, []string{"B"}) {
		t.Fatalf("no-approver actual source: %+v %v", reply, postErr)
	}
	lines, err := (&peer.Bus{Root: k.Bot.Root}).Transcript(group.ID)
	if err != nil || len(lines) != 1 || lines[0].ID != reply.MessageID || lines[0].Sender != k.Bot.Name() || lines[0].Kind != inbox.KindBot || lines[0].Text != "full agreed body" || !lines[0].Time.Equal(k.now()) {
		t.Fatalf("actual group append: %+v %v", lines, err)
	}
	b, err := bot.Load(k.Bot.Root, "B")
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	events := box.Pending()
	if len(events) != 1 || events[0].Chain != "ch_original" || events[0].Hops != 2 || events[0].SenderKind != inbox.KindBot || events[0].Sender != k.Bot.Name() || events[0].Source != "group:"+group.ID || !events[0].Untrusted || !strings.Contains(events[0].Content, "\nfull agreed body\n") {
		t.Fatalf("actual fanout event: %+v", events)
	}
	for _, callID := range []string{"", originalCall, "foreign-call-id"} {
		refused, err := cliops.CallChatGroupPost(context.Background(), endpoint, k.Bot.Root, callID, cliops.ChatGroupPost{ID: group.ID, Text: "must not append"})
		if err == nil || refused.Appended || refused.MessageID != "" || refused.PostedMembers == nil || len(refused.PostedMembers) != 0 {
			t.Fatalf("missing/completed/foreign call %q: %+v %v", callID, refused, err)
		}
	}
	if lines, err := (&peer.Bus{Root: k.Bot.Root}).Transcript(group.ID); err != nil || len(lines) != 1 {
		t.Fatalf("rejected source appended: %d %v", len(lines), err)
	}
}

func TestChatGroupRuntimePostNativeBoundsRateAndPartialFacts(t *testing.T) {
	t.Run("bounds-and-rate", func(t *testing.T) {
		k, group, endpoint := chatGroupRuntimeSource(t, "B")
		for _, text := range []string{"", " \t\n", strings.Repeat("x", peer.MaxText+1)} {
			if reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: text}); err == nil || reply.Appended || reply.Stage != "validation" {
				t.Fatalf("native text bound: %+v %v", reply, err)
			}
		}
		owner := k.ensureTurn()
		owner.mu.Lock()
		owner.turnHops = peer.MaxHops
		owner.mu.Unlock()
		if reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: "hop four"}); err == nil || reply.Appended || !strings.Contains(reply.Error, "hop limit") {
			t.Fatalf("native hop bound: %+v %v", reply, err)
		}
		owner.mu.Lock()
		owner.turnHops = 0
		owner.mu.Unlock()
		for i := 0; i < peer.RatePerMinute; i++ {
			text := "within native rate"
			if i == 0 {
				text = " \n" + strings.Repeat("x", peer.MaxText) + "\n "
			}
			reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: text})
			if err != nil || !reply.Appended || reply.Stage != "complete" {
				t.Fatalf("post %d: %+v %v", i, reply, err)
			}
		}
		if reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: "over native rate"}); err == nil || reply.Appended || reply.Stage != "rate" {
			t.Fatalf("native rate bound: %+v %v", reply, err)
		}
		lines, err := (&peer.Bus{Root: k.Bot.Root}).Transcript(group.ID)
		if err != nil || len(lines) != peer.RatePerMinute || len(lines[0].Text) != peer.MaxText {
			t.Fatalf("native writes count/body: %d %v", len(lines), err)
		}
		b, err := bot.Load(k.Bot.Root, "B")
		if err != nil {
			t.Fatal(err)
		}
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		if box.Pending()[0].Chain != "ch_"+k.Bot.Name() || box.Pending()[0].Hops != 1 {
			t.Fatalf("empty chain current-source fallback: %+v", box.Pending()[0])
		}
	})
	t.Run("actual-late-inbox-failure", func(t *testing.T) {
		k, group, endpoint := chatGroupRuntimeSource(t, "B", "C")
		c, err := bot.Load(k.Bot.Root, "C")
		if err != nil {
			t.Fatal(err)
		}
		bad := []byte("{invalid complete inbox JSON}\n")
		if err := os.WriteFile(c.InboxPath(), bad, 0600); err != nil {
			t.Fatal(err)
		}
		reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: "actual partial body"})
		if err == nil || !reply.Appended || reply.MessageID == "" || reply.ID != group.ID || !reflect.DeepEqual(reply.PostedMembers, []string{"B"}) || reply.FailedMember != "C" || reply.Stage != "recipient_open" || reply.Error == "" {
			t.Fatalf("actual partial map: %+v %v", reply, err)
		}
		lines, err := (&peer.Bus{Root: k.Bot.Root}).Transcript(group.ID)
		if err != nil || len(lines) != 1 || lines[0].ID != reply.MessageID || lines[0].Text != "actual partial body" {
			t.Fatalf("actual partial group append: %+v %v", lines, err)
		}
		b, err := bot.Load(k.Bot.Root, "B")
		if err != nil {
			t.Fatal(err)
		}
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		if len(box.Pending()) != 1 || !strings.Contains(box.Pending()[0].Content, "actual partial body") {
			t.Fatalf("B did not accept partial: %+v", box.Pending())
		}
		if got, err := os.ReadFile(c.InboxPath()); err != nil || string(got) != string(bad) {
			t.Fatalf("C was rewritten/retried: %q %v", got, err)
		}
	})
}

func TestChatGroupRuntimePostCurrentNameAfterRename(t *testing.T) {
	k, group, _ := chatGroupRuntimeSource(t, "B")
	resources := botControlKernel(t, k.Bot).Resources
	k.Resources = resources
	k.CloseOperations()
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	endpoint := operationEndpoint(k)
	if err := resources.RenameTo("renamed"); err != nil {
		t.Fatal(err)
	}
	if err := peer.RenameMember(k.Bot.Root, k.Bot.Name(), "renamed"); err != nil {
		t.Fatal(err)
	}
	reply, err := chatGroupRuntimePost(t, k, endpoint, cliops.ChatGroupPost{ID: group.ID, Text: "renamed sender"})
	if err != nil || !reply.Appended {
		t.Fatalf("renamed post: %+v %v", reply, err)
	}
	b, err := bot.Load(k.Bot.Root, "B")
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if events := box.Pending(); len(events) != 1 || events[0].Sender != "renamed" || events[0].Chain != "ch_renamed" {
		t.Fatalf("current sender/chain: %+v", events)
	}
	if _, err := os.Stat(k.Bot.InboxPath()); !os.IsNotExist(err) {
		t.Fatalf("old source path revived: %v", err)
	}
}

func TestChatGroupRuntimeActualChildPreparedTuple(t *testing.T) {
	p := &scripted{resps: []provider.Message{toolCall("child-group-source", "sh", `{"command":"\"$XBOT_BIN\" chat-group post --self --file child.txt","description":"send agreed child message"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("child complete")}}}
	k, b, box := dispatchKernel(t, p)
	chatGroupRuntimeBot(t, b.Root, "B", "u1")
	g := chatGroupRuntimeGroup(t, &peer.Bus{Root: b.Root}, b.Name(), "child room", "B")
	owner := k.ensureTurn()
	owner.mu.Lock()
	owner.turnChain, owner.turnHops = "ch_child_prepared", 1
	owner.mu.Unlock()
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.CloseOperations(); _ = k.Close() })
	endpoint := operationEndpoint(k)
	_ = k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		t.Fatalf("executor %T", k.execSnapshot())
	}
	type result struct {
		reply cliops.ChatGroupPostReply
		err   error
		chain string
		hops  int
	}
	called := make(chan result, 1)
	originalTools := local.ToolsFor
	local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
		list := originalTools(req)
		for i, tool := range list {
			if tool.Name() != "sh" {
				continue
			}
			list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				frozen, ok := ctx.Value(turnKey{}).(*turn)
				if !ok || frozen == nil {
					return "", fmt.Errorf("actual child has no prepared frozen turn")
				}
				frozen.mu.Lock()
				chain, hops := frozen.turnChain, frozen.turnHops
				frozen.mu.Unlock()
				k.turnMu.Lock()
				k.turn = &turn{turnChain: "ch_other_current", turnHops: 3}
				k.turnMu.Unlock()
				reply, err := cliops.CallChatGroupPost(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.ChatGroupPost{ID: g.ID, Text: "actual child body"})
				called <- result{reply, err, chain, hops}
				return "child post checked", nil
			}}
		}
		return list
	}
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "post the agreed group message"})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
		t.Fatalf("wrong child completed: %s", done.Sender)
	}
	select {
	case got := <-called:
		if got.err != nil || !got.reply.Appended || got.hops >= peer.MaxHops {
			t.Fatalf("actual child result: %+v", got)
		}
		peerBot, err := bot.Load(b.Root, "B")
		if err != nil {
			t.Fatal(err)
		}
		peerBox, err := inbox.Open(peerBot.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		events := peerBox.Pending()
		wantChain := got.chain
		if wantChain == "" {
			wantChain = "ch_" + b.Name()
		}
		if len(events) != 1 || events[0].Chain != wantChain || events[0].Hops != got.hops+1 || events[0].Chain == "ch_other_current" || events[0].SenderKind != inbox.KindBot {
			t.Fatalf("child borrowed current tuple: %+v", events)
		}
	default:
		t.Fatal("actual child did not reach source HTTP")
	}
}
