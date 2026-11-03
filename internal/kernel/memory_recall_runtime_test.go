package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func memoryRecallRuntimeFixture(t *testing.T) (*Kernel, string) {
	t.Helper()
	b := botControlMake(t, t.TempDir(), "source", "", "u1", "u2")
	k := botControlKernel(t, b)
	k.WorkDir = work.BotDir(b.Root, b.Name())
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
	return k, operationEndpoint(k)
}

func memoryRecallWrite(t *testing.T, root, group, id, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, group, id, "memory", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func memoryRecallReceipt(t *testing.T, reply cliops.MemoryRecallReply) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatTextResult(reply)) > tools.DefaultShellOutputBytes {
		t.Errorf("actual JSONNL/human budgets json=%d human=%d err=%v", len(raw)+1, len(cliops.FormatTextResult(reply)), err)
	}
}

// Author integration: actual Agent -> prepared Jobs -> source HTTP, with only
// the Shell process replaced. This is not independent CLI end-to-end evidence.
func TestMemoryRecallRuntimeFrozenPersonUnionAndNonprimary(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	b := k.Bot
	primary := botControlMake(t, b.Root, "primary", "u2")
	if err := bot.SetPrimary(b.Root, primary.Name()); err != nil {
		t.Fatal(err)
	}
	memoryRecallWrite(t, b.Root, "bots", b.Name(), "projects/launch.txt", "# header\nneedle BOT-BODY\n")
	memoryRecallWrite(t, b.Root, "people", "u1", "free.md", "needle PERSON-ONE\n")
	memoryRecallWrite(t, b.Root, "people", "u2", "free.md", "needle PERSON-TWO\n")
	owner := k.ensureTurn()
	owner.turnSender, owner.turnTrusted = "u1", false
	runRoutineSource(t, k, nil, `{"command":"\"$XBOT_BIN\" memory-recall --self --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if name, approve := agent.ToolApprover(ctx); name != "sh" || approve != nil {
			t.Error("fixture must exercise a real Shell without an approver function")
		}
		frozen, _ := ctx.Value(turnKey{}).(*turn)
		if frozen == nil || frozen == owner || frozen.turnSender != "u1" {
			t.Error("Shell did not receive original prepared person")
		}
		owner.mu.Lock()
		owner.turnSender = "u2"
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{turnSender: "u2", turnTrusted: true}
		k.turnMu.Unlock()
		for _, scope := range []string{"", "bot", "person"} {
			reply, err := cliops.CallMemoryRecall(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle", Scope: scope})
			if err != nil || strings.Contains(reply.Text, "PERSON-TWO") || strings.Contains(reply.Text, "[secret]") || strings.Contains(reply.Text, "incomplete") {
				t.Errorf("frozen source scope=%q reply=%+v err=%v", scope, reply, err)
			}
			if strings.Contains(reply.Text, "bot:projects/launch.txt:2: needle BOT-BODY") != (scope != "person") || strings.Contains(reply.Text, "person:free.md:1: needle PERSON-ONE") != (scope != "bot") {
				t.Errorf("original empty union/exact scope=%q text=%q", scope, reply.Text)
			}
			memoryRecallReceipt(t, reply)
		}
		return "completed", nil
	})
	if k.Provider != nil {
		t.Fatal("local scan unexpectedly required a model")
	}
}

func TestMemoryRecallRuntimeOriginalPersonKindAndSubagentPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, kind, source, sender, person string
	}{
		{"sender_untrusted", "", "", "u2", "u2"},
		{"source_subagent", "", link.SourceSubagent, "u2", "u1"},
		{"sender_subagent", "", "", "sa_original", "u1"},
		{"bot_before_subagent", link.KindBot, link.SourceSubagent, "sa_original", ""},
		{"group_before_subagent", link.KindGroup, link.SourceSubagent, "sa_original", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, endpoint := memoryRecallRuntimeFixture(t)
			for _, id := range []string{"u1", "u2"} {
				memoryRecallWrite(t, k.Bot.Root, "people", id, "free.txt", "needle "+id)
			}
			original := k.ensureTurn()
			original.turnKind, original.turnSource, original.turnSender = tc.kind, tc.source, tc.sender
			original.turnTrusted = false
			runRoutineSource(t, k, nil, `{"command":"memory-recall --self --scope person --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				frozen, _ := ctx.Value(turnKey{}).(*turn)
				if frozen == nil || frozen.turnKind != tc.kind || frozen.turnSource != tc.source || frozen.turnSender != tc.sender || k.memoryPersonOf(frozen) != tc.person {
					t.Error("original source flags/person mapping changed")
				}
				reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle", Scope: "person"})
				want := "no matches"
				if tc.person != "" {
					want = "- person:free.txt:1: needle " + tc.person
				}
				if err != nil || reply.Text != want {
					t.Errorf("original helper mapping text=%q want=%q err=%v", reply.Text, want, err)
				}
				return "completed", nil
			})
		})
	}
}

func TestMemoryRecallRuntimeActualLocalChildObservesPreparedPerson(t *testing.T) {
	p := &scripted{resps: []provider.Message{toolCall("child-recall", "sh", `{"command":"memory-recall --self --query needle"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("child complete")}}}
	k, b, box := dispatchKernel(t, p)
	if err := b.SetOwners([]string{"", "u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	k.ensureTurn().turnSender, k.ensureTurn().turnTrusted = "u2", true
	memoryRecallWrite(t, b.Root, "bots", b.Name(), "free.txt", "needle BOT")
	memoryRecallWrite(t, b.Root, "people", "u1", "free.txt", "needle ONE")
	memoryRecallWrite(t, b.Root, "people", "u2", "free.txt", "needle TWO")
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	endpoint := operationEndpoint(k)
	_ = k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		t.Fatalf("actual executor %T", k.execSnapshot())
	}
	type result struct {
		reply                              cliops.MemoryRecallReply
		err                                error
		kind, source, sender, person, want string
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
				frozen, _ := ctx.Value(turnKey{}).(*turn)
				if frozen == nil {
					return "", fmt.Errorf("actual child has no frozen turn")
				}
				identity, _ := ctx.Value(routineCallSourceKey{}).(routineCallSource)
				if !identity.child || identity.owner != nil || identity.trusted || identity.person != "" {
					t.Error("child policy marker changed")
				}
				// Observe the actual source tuple before deriving person eligibility.
				kind, source, sender := frozen.turnKind, frozen.turnSource, frozen.turnSender
				person := k.memoryPersonOf(frozen)
				want, err := k.recall(ctx, recallInput{Query: "needle"})
				if err != nil {
					return "", err
				}
				k.turnMu.Lock()
				k.turn = &turn{turnSender: "u2", turnTrusted: true}
				k.turnMu.Unlock()
				reply, err := cliops.CallMemoryRecall(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
				called <- result{reply, err, kind, source, sender, person, want}
				return "completed", err
			}}
		}
		return list
	}
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "search the agreed memory"})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
		t.Fatalf("wrong child completed: %s", done.Sender)
	}
	select {
	case got := <-called:
		t.Logf("actual Local child frozen kind=%q source=%q sender=%q native person=%q", got.kind, got.source, got.sender, got.person)
		if got.err != nil || got.reply.Text != got.want || !strings.Contains(got.reply.Text, "bot:free.txt:1: needle BOT") {
			t.Fatalf("actual child differs from native helper: %+v", got)
		}
		if strings.Contains(got.reply.Text, "person:") != (got.person != "") {
			t.Fatalf("child eligibility inferred from policy instead of actual tuple: %+v", got)
		}
	default:
		t.Fatal("actual child never reached source HTTP")
	}
}

func TestMemoryRecallRuntimeSourceMissingFinishedForeignCanceled(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	foreign := &Kernel{Bot: k.Bot, shell: k.shellJobs(), WorkDir: k.WorkDir}
	if err := foreign.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.CloseOperations)
	check := func(endpoint, id string) {
		t.Helper()
		if id == "" {
			// Bypass the client's early missing-ID check to exercise the real
			// source route, original root guard and typed runtime rejection.
			req, err := http.NewRequest(http.MethodPost, endpoint+"/v1/memory/recall", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			root, err := cliops.CanonicalRoot(k.Bot.Root)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-XBot-Root", root)
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var reply cliops.MemoryRecallReply
			if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode < 400 || !strings.Contains(reply.Error, "source approval context unavailable") || reply.Text != "" || reply.File != "" {
				t.Errorf("missing source ID accepted by runtime status=%d reply=%+v", response.StatusCode, reply)
			}
			memoryRecallReceipt(t, reply)
			return
		}
		reply, err := cliops.CallMemoryRecall(context.Background(), endpoint, k.Bot.Root, id, cliops.MemoryRecall{})
		if err == nil || reply.Error == "" || reply.Text != "" || reply.File != "" {
			t.Errorf("unavailable source accepted %+v err=%v", reply, err)
		}
		memoryRecallReceipt(t, reply)
	}
	check(endpoint, "")
	check(endpoint, "not-a-live-job")
	var completed string
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		check(operationEndpoint(foreign), completed)
		return "completed", nil
	})
	check(endpoint, completed)
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if _, err := k.liveShell().Cancel(tools.CallID(ctx)); err != nil {
			t.Error(err)
		}
		check(endpoint, tools.CallID(ctx))
		return "completed", nil
	})
}

func TestMemoryRecallRuntimeSourceRequiresFrozenAndShell(t *testing.T) {
	for _, mode := range []string{"missing_frozen", "non_shell"} {
		t.Run(mode, func(t *testing.T) {
			k, endpoint := memoryRecallRuntimeFixture(t)
			owner := k.ensureTurn()
			owner.turnSender = "u1"
			shell := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{})
				if err == nil || reply.Text != "" || reply.Error == "" {
					t.Errorf("malformed prepared source borrowed latest: %+v %v", reply, err)
				}
				return "completed", nil
			}}
			var tool tools.Tool = shell
			runTool := k.runToolCall
			if mode == "non_shell" {
				tool = chatGroupNonShell{shell}
			} else {
				runTool = func(ctx context.Context, tool tools.Tool, _ json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
					return k.shellJobs().RunCall(ctx, tools.CallRequest{Name: tool.Name(), Run: run, Prepare: func(ctx context.Context, _ string) (context.Context, func()) {
						server, release := k.holdSourceOperations()
						ctx = context.WithValue(ctx, routineCallSourceKey{}, routineCallSource{listener: server, owner: owner})
						return context.WithValue(ctx, turnKey{}, (*turn)(nil)), release
					}})
				}
			}
			ctx := context.WithValue(context.Background(), turnKey{}, owner)
			p := &scripted{resps: []provider.Message{toolCall("invalid-source", tool.Name(), `{}`)}}
			if _, err := agent.Run(ctx, p, []tools.Tool{tool}, nil, agent.Options{OneRound: true, RunTool: runTool}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMemoryRecallRuntimeExactScopeEmptyQueryAndProcessLocalDates(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	k.ensureTurn().turnSender = "u1"
	processBoundary := time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)
	zone := "Asia/Tokyo"
	profileLocation, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	profileBoundary := time.Date(2026, 11, 2, 0, 0, 0, 0, profileLocation)
	if profileBoundary.Equal(processBoundary) {
		zone = "America/Phoenix"
		profileLocation, err = time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		profileBoundary = time.Date(2026, 11, 2, 0, 0, 0, 0, profileLocation)
	}
	if profileBoundary.Equal(processBoundary) {
		t.Fatal("profile and process midnight must differ")
	}
	if _, err := k.ApplyProfileChange(bot.ProfileChange{Timezone: &zone}); err != nil {
		t.Fatal(err)
	}
	memoryRecallWrite(t, k.Bot.Root, "bots", "source", "memory.md", "# header\n（暂无）\n- (2026-11-01) needle datedpast\n- (2026-11-02) needle datedcurrent\n")
	memoryRecallWrite(t, k.Bot.Root, "bots", "source", "log/current.md", "- 2026-11-01 20:00 needle logpast\n- 2026-11-02 01:00 needle logcurrent\n")
	path := memoryRecallWrite(t, k.Bot.Root, "bots", "source", "free.txt", "needle uniquefreemtime")
	mtime := processBoundary.Add(profileBoundary.Sub(processBoundary) / 2)
	wantFree := !mtime.Before(processBoundary)
	if wantFree == !mtime.Before(profileBoundary) {
		t.Fatal("file mtime must distinguish process and profile midnight")
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		for _, query := range []string{"", "!!!", "free.txt"} {
			reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: query})
			if err != nil || reply.Text != "no matches" {
				t.Errorf("query empty/no-token/path query=%q reply=%+v err=%v", query, reply, err)
			}
		}
		for _, scope := range []string{" BOT", "Bot", "bot ", " person", "all"} {
			reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Scope: scope})
			if err == nil || reply.Error != "memory: scope must be bot or person" {
				t.Errorf("scope normalized %q: %+v %v", scope, reply, err)
			}
		}
		for _, since := range []string{"2026-11-02 ", "2026-02-30"} {
			if reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Since: since}); err == nil || reply.Error == "" {
				t.Errorf("invalid exact date accepted: %+v %v", reply, err)
			}
		}
		reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle", Since: "2026-11-02"})
		if err != nil || !strings.Contains(reply.Text, "datedcurrent") || !strings.Contains(reply.Text, "logcurrent") || strings.Contains(reply.Text, "datedpast") || strings.Contains(reply.Text, "logpast") || strings.Contains(reply.Text, "uniquefreemtime") != wantFree {
			t.Errorf("process Local versus profile zone/line date: %+v err=%v wantFree=%v process=%s profile=%s mtime=%s", reply, err, wantFree, processBoundary, profileBoundary, mtime)
		}
		return "completed", nil
	})
}

func TestMemoryRecallRuntimeTwentyHitsUnicodeRemainingAndNoPathTokens(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	body := strings.Repeat("needle "+strings.Repeat("界", 220)+"\n", 23)
	memoryRecallWrite(t, k.Bot.Root, "bots", "source", "projects/pathonly.txt", body)
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
		if err != nil || strings.Count(reply.Text, "- bot:") != 20 || !strings.HasSuffix(reply.Text, "(3 more; narrow the query or use since=)") || strings.Contains(reply.Text, "incomplete") {
			t.Fatalf("original hit/remaining text: %+v %v", reply, err)
		}
		for _, line := range strings.Split(reply.Text, "\n")[:20] {
			_, text, ok := strings.Cut(line, ": ")
			if !ok || len([]rune(text)) != 201 || !strings.HasSuffix(text, "…") || !strings.HasPrefix(text, "needle ") {
				t.Errorf("200 Unicode runes plus ellipsis changed: %q", line)
			}
		}
		memoryRecallReceipt(t, reply)
		return "completed", nil
	})
}

func TestMemoryRecallRuntimeUnionSharedByteBudgetAndPerFileTruncation(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global_%v", global), func(t *testing.T) {
			k, endpoint := memoryRecallRuntimeFixture(t)
			k.ensureTurn().turnSender = "u1"
			body := "needle " + strings.Repeat("x", (256<<10)-len("needle "))
			count := 1
			if global {
				count = 16
			}
			for i := 0; i < count; i++ {
				memoryRecallWrite(t, k.Bot.Root, "bots", "source", fmt.Sprintf("%02d.txt", i), body+"\nneedle hidden")
			}
			memoryRecallWrite(t, k.Bot.Root, "people", "u1", "free.txt", "personmarker")
			runRoutineSource(t, k, nil, `{"command":"memory-recall --self --query personmarker"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "personmarker"})
				if err != nil || strings.Contains(reply.Text, "person:") == global || strings.Contains(reply.Text, "search incomplete") != global {
					t.Errorf("shared bot-first budget/perfile incomplete: %+v %v", reply, err)
				}
				return "completed", nil
			})
		})
	}
}
