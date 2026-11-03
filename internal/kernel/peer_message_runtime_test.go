package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/memory"
	"xbot/internal/peer"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func peerMessageRuntimeFixture(t *testing.T) (*Kernel, string) {
	t.Helper()
	k, _, _ := freshBot(t)
	if err := k.Bot.SetOwners([]string{"one", "shared"}); err != nil {
		t.Fatal(err)
	}
	k.WorkDir = t.TempDir()
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	return k, operationEndpoint(k)
}

func peerMessageRuntimeReceipt(t *testing.T, reply cliops.PeerMessageReply) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatPeerMessage(reply)) > tools.DefaultShellOutputBytes {
		t.Errorf("receipt exceeds actual Shell budget: json=%d human=%d err=%v", len(raw)+1, len(cliops.FormatPeerMessage(reply)), err)
	}
}

func TestPeerMessageRuntimeLocalFrozenSharedNonprimary(t *testing.T) {
	k, endpoint := peerMessageRuntimeFixture(t)
	b := k.Bot
	target := botControlMake(t, b.Root, "B", "shared", "other")
	if err := bot.SetPrimary(b.Root, target.Name()); err != nil {
		t.Fatal(err)
	}
	// The nil-resource source snapshot must not open or scan this Inbox.
	if err := os.WriteFile(b.InboxPath(), []byte("invalid inbox JSON\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := k.ensureTurn()
	owner.turnChain, owner.turnHops = "ch_original", 2
	body := strings.Repeat("界", 2730) + "ab"
	var posted cliops.PeerMessageReply
	runRoutineSource(t, k, nil, `{"command":"peer-message B --self --file message.txt"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		frozen, _ := ctx.Value(turnKey{}).(*turn)
		if frozen == nil || frozen == owner {
			t.Error("source is not a real prepared frozen context")
		}
		owner.mu.Lock()
		owner.turnChain, owner.turnHops, owner.branch = "ch_changed", 3, "bg_original"
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{turnChain: "ch_latest", turnHops: 0, branch: "latest"}
		k.turnMu.Unlock()
		var err error
		posted, err = cliops.CallPeerMessage(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: " \tB\n", Text: " \n" + body + "\t "})
		if err != nil || !posted.Posted || posted.EventID == "" || posted.Bot != "B" || posted.Route != "local" || posted.Stage != "complete" || posted.Recorded != nil || posted.RecordSeq != 0 || posted.Gateway != "" || posted.Queued {
			t.Errorf("local acceptance facts lost: %+v err=%v", posted, err)
		}
		peerMessageRuntimeReceipt(t, posted)
		return "completed", nil
	})
	box, err := inbox.Open(target.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	pending := box.Pending()
	if len(pending) != 1 {
		t.Fatalf("actual recipient events=%d", len(pending))
	}
	ev := pending[0]
	if ev.ID != posted.EventID || ev.Content != body || ev.Source != "bot:"+b.Name() || ev.Sender != b.Name() || ev.SenderKind != inbox.KindBot || !ev.Untrusted || ev.Chain != "ch_original" || ev.Hops != 3 || !ev.Time.Equal(k.now()) {
		t.Fatal("recipient event lost original sender/body/chain/hops/time metadata")
	}
	if rows, err := peer.Conversation(b, target); err != nil || len(rows) != 0 {
		t.Fatalf("pending Inbox misrepresented as transcript delivery: rows=%d err=%v", len(rows), err)
	}
	recipient := &Kernel{Bot: target, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("SILENT")}}}, Memory: memory.NopStore{}, Now: k.Now}
	if err := recipient.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	rows, err := peer.Conversation(b, target)
	if err != nil || len(rows) != 1 || rows[0].ID != posted.EventID || rows[0].Text != body || rows[0].From != b.Name() || rows[0].To != target.Name() {
		t.Fatalf("original Conversation consumer: rows=%d err=%v", len(rows), err)
	}
	entries, err := b.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("local path wrote an extra source record: entries=%d err=%v", len(entries), err)
	}
}

func TestPeerMessageRuntimeLocalValidationAndRate(t *testing.T) {
	for _, tc := range []struct {
		name, target, text string
		hops               int
	}{
		{"self", "ada", "body", 0}, {"empty_target", " \t", "body", 0},
		{"foreign", "foreign", "body", 0}, {"orphan", "orphan", "body", 0},
		{"empty", "B", " \n\t", 0}, {"oversize", "B", strings.Repeat("x", peer.MaxText+1), 0},
		{"hop_four", "B", "body", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, endpoint := peerMessageRuntimeFixture(t)
			b := k.Bot
			botControlMake(t, b.Root, "B", "shared")
			botControlMake(t, b.Root, "foreign", "other")
			botControlMake(t, b.Root, "orphan")
			k.ensureTurn().turnHops = tc.hops
			runRoutineSource(t, k, nil, `{"command":"peer-message --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallPeerMessage(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: tc.target, Text: tc.text})
				if err == nil || reply.Posted || reply.EventID != "" || reply.Recorded != nil || reply.Stage != "validation" {
					t.Errorf("invalid local send accepted: %+v err=%v", reply, err)
				}
				peerMessageRuntimeReceipt(t, reply)
				return "completed", nil
			})
			if _, err := os.Stat(filepath.Join(b.Root, "peer-rate.json")); !os.IsNotExist(err) {
				t.Fatalf("validation consumed rate: %v", err)
			}
		})
	}
	k, endpoint := peerMessageRuntimeFixture(t)
	target := botControlMake(t, k.Bot.Root, "B", "shared")
	runRoutineSource(t, k, nil, `{"command":"peer-message B --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		for i := 0; i < 9; i++ {
			reply, err := cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B", Text: "body"})
			if i < 8 && (err != nil || !reply.Posted || reply.EventID == "") || i == 8 && (err == nil || reply.Posted || reply.EventID != "" || reply.Stage != "rate") {
				t.Errorf("original rate attempt %d: %+v err=%v", i+1, reply, err)
			}
		}
		return "completed", nil
	})
	box, err := inbox.Open(target.InboxPath())
	if err != nil || len(box.Pending()) != 8 {
		t.Fatalf("original rate accepted events=%d err=%v", len(box.Pending()), err)
	}
}

func TestPeerMessageRuntimeSourceMissingFinishedForeignCanceled(t *testing.T) {
	k, endpoint := peerMessageRuntimeFixture(t)
	b := k.Bot
	botControlMake(t, b.Root, "B", "shared")
	foreign := &Kernel{Bot: b, shell: k.shellJobs(), WorkDir: k.WorkDir}
	if err := foreign.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.CloseOperations)
	check := func(endpoint, id string) {
		t.Helper()
		reply, err := cliops.CallPeerMessage(context.Background(), endpoint, b.Root, id, cliops.PeerMessage{Bot: "B", Text: "body"})
		if err == nil || reply.Posted || reply.EventID != "" || reply.Recorded != nil {
			t.Errorf("unavailable source accepted: %+v err=%v", reply, err)
		}
	}
	check(endpoint, "")
	check(endpoint, "not-an-actual-job")
	var completed string
	runRoutineSource(t, k, nil, `{"command":"peer-message B --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		check(operationEndpoint(foreign), completed)
		return "completed", nil
	})
	check(endpoint, completed)
	runRoutineSource(t, k, nil, `{"command":"peer-message B --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		id := tools.CallID(ctx)
		if _, err := k.liveShell().Cancel(id); err != nil {
			t.Error(err)
		}
		check(endpoint, id)
		return "completed", nil
	})
	if _, err := os.Stat(filepath.Join(b.Root, "peer-rate.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid source reached rate writer: %v", err)
	}
}

func TestPeerMessageRuntimeLocalCurrentRenameAndEmptyChain(t *testing.T) {
	root := t.TempDir()
	b := botControlMake(t, root, "source", "shared")
	target := botControlMake(t, root, "B", "shared")
	k := botControlKernel(t, b)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
	endpoint := operationEndpoint(k)
	runRoutineSource(t, k, nil, `{"command":"peer-message B --self --text body"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if err := k.Resources.RenameTo("renamed"); err != nil {
			t.Error(err)
			return "", err
		}
		reply, err := cliops.CallPeerMessage(ctx, endpoint, root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B", Text: "body"})
		if err != nil || !reply.Posted {
			t.Errorf("same listener after rename: %+v err=%v", reply, err)
		}
		return "completed", nil
	})
	box, err := inbox.Open(target.InboxPath())
	if err != nil || len(box.Pending()) != 1 {
		t.Fatalf("renamed delivery missing: %v", err)
	}
	ev := box.Pending()[0]
	if ev.Sender != "renamed" || ev.Source != "bot:renamed" || ev.Chain != "ch_renamed" || ev.Hops != 1 {
		t.Fatal("renamed delivery used stale source name or an invented chain")
	}
	if _, err := os.Stat(bot.Dir(root, "source")); !os.IsNotExist(err) {
		t.Fatalf("old source directory revived: %v", err)
	}
}

// Exercise malformed prepared Jobs through the actual Agent/Jobs/HTTP boundary.
// Only these negative fixtures omit one identity the normal Prepare always sets.
func TestPeerMessageRuntimeSourceRequiresExplicitFrozenAndExecution(t *testing.T) {
	for _, missing := range []string{"frozen", "execution"} {
		t.Run(missing, func(t *testing.T) {
			k, endpoint := peerMessageRuntimeFixture(t)
			owner := k.ensureTurn()
			tool := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B@missing", Text: "body"})
				if err == nil || reply.Posted || reply.EnvelopeID != "" || reply.Recorded != nil || !strings.Contains(reply.Error, "source "+missing+" turn unavailable") {
					t.Errorf("missing explicit %s borrowed a fallback: %+v err=%v", missing, reply, err)
				}
				return "completed", nil
			}}
			ctx := context.WithValue(context.Background(), turnKey{}, owner)
			p := &scripted{resps: []provider.Message{toolCall("malformed-prepared", "sh", `{}`)}}
			_, err := agent.Run(ctx, p, []tools.Tool{tool}, nil, agent.Options{OneRound: true, RunTool: func(ctx context.Context, tool tools.Tool, _ json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
				return k.shellJobs().RunCall(ctx, tools.CallRequest{Name: tool.Name(), Run: run, Prepare: func(ctx context.Context, _ string) (context.Context, func()) {
					server, release := k.holdSourceOperations()
					ctx = context.WithValue(ctx, routineCallSourceKey{}, routineCallSource{listener: server, owner: owner})
					if missing == "frozen" {
						ctx = context.WithValue(ctx, turnKey{}, (*turn)(nil))
						ctx = context.WithValue(ctx, callTurnKey{}, owner)
					} else {
						ctx = context.WithValue(ctx, turnKey{}, &turn{})
						ctx = context.WithValue(ctx, callTurnKey{}, (*turn)(nil))
					}
					return ctx, release
				}})
			}})
			if err != nil {
				t.Fatal(err)
			}
			entries, err := k.Bot.Entries()
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid prepared Job wrote an outgoing record: count=%d err=%v", len(entries), err)
			}
			if _, err := os.Stat(filepath.Join(k.Bot.Root, "peer-rate.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid prepared Job consumed rate: %v", err)
			}
		})
	}
}

func TestPeerMessageRuntimeSourceNonShell(t *testing.T) {
	k, endpoint := peerMessageRuntimeFixture(t)
	botControlMake(t, k.Bot.Root, "B", "shared")
	shell := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallPeerMessage(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.PeerMessage{Bot: "B", Text: "body"})
		if err == nil || reply.Posted || reply.Recorded != nil {
			t.Errorf("non-Shell source accepted: %+v err=%v", reply, err)
		}
		return "completed", nil
	}}
	tool := chatGroupNonShell{shell}
	ctx := context.WithValue(context.Background(), turnKey{}, k.ensureTurn())
	p := &scripted{resps: []provider.Message{toolCall("non-shell", tool.Name(), `{}`)}}
	if _, err := agent.Run(ctx, p, []tools.Tool{tool}, nil, agent.Options{OneRound: true, RunTool: k.runToolCall}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(k.Bot.Root, "peer-rate.json")); !os.IsNotExist(err) {
		t.Fatalf("non-Shell source consumed rate: %v", err)
	}
}
