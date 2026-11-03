package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/clip"
	"xbot/internal/compact"
	"xbot/internal/envprompt"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func compactRuntimeFixture(t *testing.T) (*Kernel, string) {
	t.Helper()
	return experienceRuntimeFixture(t, true)
}

// Four finished records, including a real spilled result, followed by exactly
// the default eight raw tail records. This keeps range and preview boundaries
// observable without copying the domain's full range-guard suite.
func compactRuntimeSeed(t *testing.T, k *Kernel) {
	t.Helper()
	experienceRuntimeAppend(t, k.Bot,
		bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("PROJECT_DECISION ship Friday with result seq 3")}},
		bot.Entry{Message: toolCall("finished", "inspect", `{"path":"project.txt"}`)},
		bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("finished", "STORED_RESULT_PREVIEW "+strings.Repeat("p", 600)+" DEEP_BLOB_PAYLOAD "+strings.Repeat("z", 5000))}},
		bot.Entry{Images: []string{"diagram.png"}, Audios: []string{"meeting.wav"}, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("FINISHED_CONCLUSION retain the launch decision")}},
	)
	for i := 0; i < 8; i++ {
		experienceRuntimeAppend(t, k.Bot, bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(fmt.Sprintf("RAW_TAIL_%d", i))}})
	}
}

func compactRuntimeBudget(t *testing.T, reply cliops.CompactReply) {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil || len(raw)+1 > tools.DefaultShellOutputBytes || len(cliops.FormatCompact(reply)) > tools.DefaultShellOutputBytes || len(raw)+1 > cliops.CompactMaxReply {
		t.Fatalf("compact JSONNL/human/HTTP budgets json=%d human=%d err=%v", len(raw)+1, len(cliops.FormatCompact(reply)), err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for name := range fields {
		if name != "text" && name != "error" {
			t.Fatalf("short compact reply introduced an extra fact/artifact field: %s", name)
		}
	}
}

func TestCompactRuntimePreparedSourceLLMUsageAndPublication(t *testing.T) {
	k, endpoint := compactRuntimeFixture(t)
	primary := botControlMake(t, k.Bot.Root, "primary", "unrelated-owner")
	if err := bot.SetPrimary(k.Bot.Root, primary.Name()); err != nil {
		t.Fatal(err)
	}
	compactRuntimeSeed(t, k)
	store := memory.Open(k.Bot.Root)
	k.Memory = store
	for _, fact := range []memory.Fact{
		{Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "source", WrittenBy: "source", Key: "project", Text: "PROFILE_MEMORY_UNCHANGED", Time: k.now()},
		{Scope: memory.ScopeBot, Tier: memory.TierLog, BotName: "source", WrittenBy: "source", Key: "progress", Text: "LOG_MEMORY_UNCHANGED", Time: k.now()},
	} {
		if err := store.Remember(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	memoryDir := filepath.Join(bot.Dir(k.Bot.Root, "source"), "memory")
	logs, err := filepath.Glob(filepath.Join(memoryDir, "log", "*.md"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("actual memory log fixture: %v err=%v", logs, err)
	}
	memoryBefore := map[string][]byte{}
	for _, path := range append([]string{filepath.Join(memoryDir, "memory.md")}, logs...) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		memoryBefore[path] = raw
	}
	before, err := k.Bot.Entries()
	if err != nil || len(before) != 12 || before[2].Blob == "" || strings.Contains(before[2].Message.Text(), "DEEP_BLOB_PAYLOAD") {
		t.Fatalf("stored preview/default raw tail fixture: records=%d err=%v", len(before), err)
	}
	owner := k.ensureTurn()
	owner.fromSeq, owner.turnKind = 1, link.KindBot
	type modelObservation struct {
		request    provider.Request
		turn       int
		approver   string
		hasApprove bool
		frozen     *turn
	}
	modelSeen := make(chan modelObservation, 1)
	modelText := "COMPACT_SUMMARY_BEGIN " + strings.Repeat("s", 9000) + " COMPACT_SUMMARY_END"
	k.Compactor = compact.LLM{P: providerFunc(func(ctx context.Context, request provider.Request, _ func(string)) (provider.Message, error) {
		name, approve := agent.ToolApprover(ctx)
		frozen, _ := ctx.Value(turnKey{}).(*turn)
		modelSeen <- modelObservation{snapshotReq(request), turnSeqOf(ctx), name, approve != nil, frozen}
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(modelText), Usage: provider.TokenUsage{PromptTokens: 73, CompletionTokens: 11, CachedTokens: 7, Reported: true, CacheReported: true}}, nil
	})}
	type publicationObservation struct {
		state bot.State
		err   error
	}
	publicationSeen := make(chan publicationObservation, 1)
	k.registeredEnvironment().Register(envprompt.Field{Key: "compact-publication-observer", Sample: func(context.Context) (envprompt.Value, error) {
		state, err := k.currentState()
		publicationSeen <- publicationObservation{state, err}
		return "publication observed", err
	}, Prompt: func(envprompt.Value, envprompt.Change) string { return "COMPACT_PUBLICATION_OBSERVER" }})
	var reply cliops.CompactReply
	var callErr error
	var prepared *turn
	runRoutineSource(t, k, nil, `{"command":"compact --self --from 1 --to 4 --focus-file focus.txt"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		prepared, _ = ctx.Value(turnKey{}).(*turn)
		owner.mu.Lock()
		owner.fromSeq = 991
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{fromSeq: 992, turnKind: link.KindGroup}
		k.turnMu.Unlock()
		reply, callErr = cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 4, Focus: " \n shipping focus \t"})
		return "completed", nil
	})
	if callErr != nil || reply.Text != "compacted 1-4" || reply.Error != "" || cliops.FormatCompact(reply) != "compacted 1-4\n" {
		t.Fatalf("original short acknowledgement: %+v err=%v", reply, callErr)
	}
	compactRuntimeBudget(t, reply)
	var observation modelObservation
	select {
	case observation = <-modelSeen:
	default:
		t.Fatal("successful compact acknowledgement did not invoke the actual LLM")
	}
	request := observation.request
	if observation.turn != 1 || observation.frozen == nil || observation.frozen != prepared || observation.frozen == owner || observation.approver != "sh" || observation.hasApprove || len(request.Messages) != 2 || request.Messages[0].Role != provider.RoleSystem || request.Messages[1].Role != provider.RoleUser || len(request.ToolDefinitions()) != 0 {
		t.Fatalf("original prepared context/no-tools LLM shape: turn=%d frozen=%p prepared=%p approver=%q tools=%d messages=%d", observation.turn, observation.frozen, prepared, observation.approver, len(request.ToolDefinitions()), len(request.Messages))
	}
	data := request.Messages[1].Text()
	for _, piece := range []string{"PROJECT_DECISION", "STORED_RESULT_PREVIEW", "FINISHED_CONCLUSION", "Focus: shipping focus", summaryFacts, filepath.Join(work.BotDir(k.Bot.Root, "source"), work.PastedDir, "diagram.png"), filepath.Join(work.BotDir(k.Bot.Root, "source"), work.VoiceDir, "meeting.wav")} {
		if !strings.Contains(data, piece) {
			t.Errorf("actual compact.LLM input missing %q", piece)
		}
	}
	if strings.Contains(data, "DEEP_BLOB_PAYLOAD") || strings.Contains(data, "RAW_TAIL_") {
		t.Fatal("compact hydrated the stored preview or summarized the protected raw tail")
	}
	var publication publicationObservation
	select {
	case publication = <-publicationSeen:
	default:
		t.Fatal("successful compact did not reach actual environment publication")
	}
	if publication.err != nil || len(publication.state.Blocks) != 0 {
		t.Fatalf("coverage saved before environment publication: %+v err=%v", publication.state.Blocks, publication.err)
	}
	state, err := k.Bot.LoadState()
	wantSummary, _ := clip.Bytes(modelText, 8<<10)
	if err != nil || len(state.Blocks) != 1 || state.Blocks[0].From != 1 || state.Blocks[0].To != 4 || state.Blocks[0].Focus != "shipping focus" || state.Blocks[0].Text != wantSummary || len(state.Blocks[0].Text) != 8<<10 || !state.Blocks[0].At.Equal(k.now()) || !state.MemSnapSet || !strings.Contains(state.MemSnap, "PROFILE_MEMORY_UNCHANGED") || !strings.Contains(state.MemSnap, "LOG_MEMORY_UNCHANGED") {
		t.Fatalf("original block/LLM8KiB/frozen memory result: blocks=%d memSet=%v err=%v", len(state.Blocks), state.MemSnapSet, err)
	}
	after, err := k.Bot.Entries()
	if err != nil || len(after) <= len(before) || !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatalf("compaction changed original transcript rows: before=%d after=%d err=%v", len(before), len(after), err)
	}
	fullSnapshot := false
	for i, entry := range after[len(before):] {
		if update, ok := decodeEnvironment(entry.Message.Text()); ok && update.Snapshot != "" {
			fullSnapshot = entry.IsInternal() && len(before)+i+1 > state.Blocks[0].To && update.Snapshot == environmentBasis(state, nil)
		}
	}
	if !fullSnapshot {
		t.Fatal("full environment snapshot is missing outside the newly covered range")
	}
	for path, before := range memoryBefore {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("compact wrote memory/log body %s: %v", path, err)
		}
	}
	logsAfter, err := filepath.Glob(filepath.Join(memoryDir, "log", "*.md"))
	if err != nil || !reflect.DeepEqual(logs, logsAfter) {
		t.Fatalf("compact created another log body: %v err=%v", logsAfter, err)
	}
	traces, err := k.Bot.Traces()
	if err != nil || len(traces) != 1 || traces[0].Call != bot.TraceCallCompact || traces[0].Turn != 1 || traces[0].Prompt != 73 || traces[0].Completion != 11 || traces[0].Cached != 7 || traces[0].Estimated || !traces[0].CacheReported {
		t.Fatalf("reported compact usage lost original prepared turn: %+v err=%v", traces, err)
	}
}

func TestCompactRuntimeSourceCancellationAndModelOutcomes(t *testing.T) {
	// The resolver's full source-ID family is already covered elsewhere. This
	// raw request proves this actual route reaches its mandatory source lookup.
	k, endpoint := compactRuntimeFixture(t)
	request, err := http.NewRequest(http.MethodPost, endpoint+"/v1/compact", strings.NewReader(`{"from":1,"to":1}`))
	if err != nil {
		t.Fatal(err)
	}
	root, err := cliops.CanonicalRoot(k.Bot.Root)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-XBot-Root", root)
	request.Header.Set("Content-Type", "application/json")
	client := cliops.LocalHTTPClient(5 * time.Second)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var missing cliops.CompactReply
	decodeErr := json.NewDecoder(response.Body).Decode(&missing)
	response.Body.Close()
	client.CloseIdleConnections()
	if decodeErr != nil || response.StatusCode < 400 || missing.Error != "source approval context unavailable" || missing.Text != "" {
		t.Fatalf("actual endpoint bypassed source lookup: status=%d reply=%+v decode=%v", response.StatusCode, missing, decodeErr)
	}
	compactRuntimeBudget(t, missing)
	for _, outcome := range []string{"cancel_source", "provider_error", "empty_summary"} {
		t.Run(outcome, func(t *testing.T) {
			k, endpoint := compactRuntimeFixture(t)
			compactRuntimeSeed(t, k)
			type observation struct {
				cancelErr, contextErr error
			}
			seen := make(chan observation, 1)
			k.Compactor = compact.LLM{P: providerFunc(func(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
				switch outcome {
				case "cancel_source":
					_, cancelErr := k.liveShell().Cancel(tools.CallID(ctx))
					if cancelErr != nil {
						seen <- observation{cancelErr: cancelErr}
						return provider.Message{}, cancelErr
					}
					select {
					case <-ctx.Done():
						seen <- observation{contextErr: ctx.Err()}
						return provider.Message{}, ctx.Err()
					case <-time.After(3 * time.Second):
						err := errors.New("original source cancellation did not reach Chat")
						seen <- observation{cancelErr: err}
						return provider.Message{}, err
					}
				case "provider_error":
					seen <- observation{}
					return provider.Message{}, errors.New("SCRIPTED_COMPACT_PROVIDER_FAILURE")
				default:
					seen <- observation{}
					return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(" \n\t "), Usage: provider.TokenUsage{PromptTokens: 41, CompletionTokens: 3, Reported: true}}, nil
				}
			})}
			var reply cliops.CompactReply
			var callErr error
			runRoutineSource(t, k, nil, `{"command":"compact --self --from 1 --to 4"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				// Keep HTTP alive when Cancel cancels this real original Shell.
				reply, callErr = cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 4})
				return "completed", nil
			})
			var observed observation
			select {
			case observed = <-seen:
			default:
				t.Fatalf("model outcome did not reach actual Chat: reply=%+v err=%v", reply, callErr)
			}
			if observed.cancelErr != nil || outcome == "cancel_source" && !errors.Is(observed.contextErr, context.Canceled) {
				t.Fatalf("source cancellation was not propagated through the model ctx: %+v", observed)
			}
			wantError := map[string]string{"cancel_source": "context canceled", "provider_error": "SCRIPTED_COMPACT_PROVIDER_FAILURE", "empty_summary": "compact: empty summary"}[outcome]
			if callErr == nil || reply.Text != "" || reply.Error != wantError {
				t.Fatalf("original model failure outcome: %+v err=%v", reply, callErr)
			}
			compactRuntimeBudget(t, reply)
			state, err := k.Bot.LoadState()
			if err != nil || len(state.Blocks) != 0 {
				t.Fatalf("model failure installed a summary block: %+v err=%v", state.Blocks, err)
			}
			traces, err := k.Bot.Traces()
			wantUsage := 0
			if outcome == "empty_summary" {
				wantUsage = 1
			}
			if err != nil || len(traces) != wantUsage || wantUsage == 1 && (traces[0].Call != bot.TraceCallCompact || traces[0].Prompt != 41 || traces[0].Completion != 3) {
				t.Fatalf("Chat nil-error accounting precedes empty-summary rejection: %+v err=%v", traces, err)
			}
		})
	}
	// A real prepared Shell can have no frozen turn value. Unlike file Recall,
	// compact adds no such gate: the original turnSeqOf must remain exactly 0.
	t.Run("nil_frozen_turn_keeps_zero_usage", func(t *testing.T) {
		k, endpoint := compactRuntimeFixture(t)
		compactRuntimeSeed(t, k)
		owner := k.ensureTurn()
		owner.fromSeq = 999
		turnSeen := make(chan int, 1)
		k.Compactor = compact.LLM{P: providerFunc(func(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
			turnSeen <- turnSeqOf(ctx)
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ZERO_TURN_SUMMARY"), Usage: provider.TokenUsage{PromptTokens: 17, CompletionTokens: 2, Reported: true}}, nil
		})}
		var reply cliops.CompactReply
		var callErr error
		shell := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			reply, callErr = cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 4})
			return "completed", nil
		}}
		runTool := func(ctx context.Context, tool tools.Tool, _ json.RawMessage, run func(context.Context) tools.CallResult) tools.CallResult {
			return k.shellJobs().RunCall(ctx, tools.CallRequest{Name: tool.Name(), Run: run, Prepare: func(ctx context.Context, _ string) (context.Context, func()) {
				server, release := k.holdSourceOperations()
				ctx = context.WithValue(ctx, routineCallSourceKey{}, routineCallSource{listener: server, owner: owner})
				return context.WithValue(ctx, turnKey{}, (*turn)(nil)), release
			}})
		}
		model := &scripted{resps: []provider.Message{toolCall("zero-turn", "sh", `{"command":"compact --self --from 1 --to 4"}`)}}
		_, err := agent.Run(context.WithValue(context.Background(), turnKey{}, owner), model, []tools.Tool{shell}, nil, agent.Options{OneRound: true, RunTool: runTool})
		if err != nil || callErr != nil || reply.Text != "compacted 1-4" {
			t.Fatalf("compact gained Recall frozen-turn gate: %+v err=%v/%v", reply, err, callErr)
		}
		select {
		case seq := <-turnSeen:
			if seq != 0 {
				t.Fatalf("nil frozen turn fell back to latest turn: %d", seq)
			}
		default:
			t.Fatal("nil frozen turn did not reach actual LLM")
		}
		traces, err := k.Bot.Traces()
		if err != nil || len(traces) != 1 || traces[0].Call != bot.TraceCallCompact || traces[0].Turn != 0 {
			t.Fatalf("original true zero usage turn was rebound: %+v err=%v", traces, err)
		}
	})
}

func TestCompactRuntimeLateStateFailureRetainsUsageAndEnvironment(t *testing.T) {
	k, endpoint := compactRuntimeFixture(t)
	compactRuntimeSeed(t, k)
	initial := bot.State{MemRender: memRenderVersion, MemSnapSet: true, MemSnap: "BEFORE_COMPACT_SNAPSHOT"}
	if err := k.Bot.SaveState(initial); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	k.Compactor = compact.LLM{P: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("STATE_SAVE_ATTEMPT_SUMMARY"), Usage: provider.TokenUsage{PromptTokens: 83, CompletionTokens: 13, Reported: true}}}}}
	type observation struct {
		state bot.State
		err   error
	}
	faultSeen := make(chan observation, 1)
	// The existing field sampler runs after refreshMemSnap's state/replay reads
	// and before environment append. Blocking only the final State target leaves
	// the actual environment appends and reported model usage observable.
	k.registeredEnvironment().Register(envprompt.Field{Key: "compact-state-write-fault", Sample: func(context.Context) (envprompt.Value, error) {
		state, err := k.currentState()
		if err == nil {
			err = os.Rename(statePath, statePath+".saved")
		}
		if err == nil {
			err = os.Mkdir(statePath, 0700)
		}
		faultSeen <- observation{state, err}
		return "STATE_SAVE_FAULT_REACHED", err
	}, Prompt: func(envprompt.Value, envprompt.Change) string { return "STATE_SAVE_FAULT_REACHED" }})
	var reply cliops.CompactReply
	var callErr error
	runRoutineSource(t, k, nil, `{"command":"compact --self --from 1 --to 4"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, callErr = cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 4})
		return "completed", nil
	})
	var fault observation
	select {
	case fault = <-faultSeen:
	default:
		t.Fatalf("compact failed before the intended State-save fault: reply=%+v err=%v", reply, callErr)
	}
	if fault.err != nil || len(fault.state.Blocks) != 0 || fault.state.MemSnap != initial.MemSnap {
		t.Fatalf("late fault did not occur after original state read: %+v err=%v", fault.state.Blocks, fault.err)
	}
	if callErr == nil || reply.Text != "" || reply.Error == "" || !strings.Contains(reply.Error, "state.json") {
		t.Fatalf("actual late State failure returned false success: %+v err=%v", reply, callErr)
	}
	compactRuntimeBudget(t, reply)
	info, err := os.Stat(statePath)
	if err != nil || !info.IsDir() {
		t.Fatalf("final State target was not the actual directory obstruction: %v err=%v", info, err)
	}
	saved, err := os.ReadFile(statePath + ".saved")
	if err != nil || !bytes.Equal(before, saved) {
		t.Fatalf("fault setup did not preserve exact pre-compaction State: %v", err)
	}
	traces, err := k.Bot.Traces()
	if err != nil || len(traces) != 1 || traces[0].Call != bot.TraceCallCompact || traces[0].Prompt != 83 || traces[0].Completion != 13 {
		t.Fatalf("late error erased already-reported model usage: %+v err=%v", traces, err)
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	full, notice := false, false
	for _, entry := range entries[12:] {
		if update, ok := decodeEnvironment(entry.Message.Text()); ok && update.Snapshot != "" && entry.IsInternal() {
			full = true
		}
		notice = notice || strings.Contains(entry.Message.Text(), "STATE_SAVE_FAULT_REACHED")
	}
	if !full || !notice {
		t.Fatalf("State error hid prior actual environment publication: full=%v sampledNotice=%v", full, notice)
	}
}

func TestCompactRuntimeModelWaitRenameUsesCurrentStateAndTrace(t *testing.T) {
	k, endpoint := compactRuntimeFixture(t)
	compactRuntimeSeed(t, k)
	if _, _, err := work.Open(k.Bot.Root, k.Bot.Name()); err != nil {
		t.Fatal(err)
	}
	original := k.Bot
	k.ensureTurn().fromSeq = 23
	type observation struct {
		turn int
		req  provider.Request
	}
	entered := make(chan observation, 1)
	release := make(chan struct{})
	k.Compactor = compact.LLM{P: providerFunc(func(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
		entered <- observation{turnSeqOf(ctx), snapshotReq(req)}
		select {
		case <-release:
			return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("RENAMED_CURRENT_STATE_SUMMARY"), Usage: provider.TokenUsage{PromptTokens: 97, CompletionTokens: 19, Reported: true}}, nil
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		}
	})}
	type result struct {
		reply cliops.CompactReply
		err   error
	}
	called := make(chan result, 1)
	shell := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallCompact(context.Background(), endpoint, original.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 4})
		called <- result{reply, err}
		return "completed", nil
	}}
	owner := k.ensureTurn()
	model := &scripted{resps: []provider.Message{toolCall("rename-compact", "sh", `{"command":"compact --self --from 1 --to 4"}`)}}
	agentDone := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.WithValue(context.Background(), turnKey{}, owner), model, []tools.Tool{shell}, nil, agent.Options{OneRound: true, RunTool: k.runToolCall})
		agentDone <- err
	}()
	var observed observation
	select {
	case observed = <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		k.Resources.Jobs.Close()
		<-agentDone
		t.Fatal("actual compact.LLM did not reach its model wait")
	}
	// The model stays blocked while the original public profile operation moves
	// this source. No callback invokes Fatal or mutates a production test seam.
	name := "renamed"
	type moveResult struct {
		profile cliops.Profile
		err     error
	}
	moveDone := make(chan moveResult, 1)
	go func() {
		profile, err := k.ApplyProfileChange(bot.ProfileChange{Name: &name})
		moveDone <- moveResult{profile, err}
	}()
	var moved moveResult
	moveBlocked := false
	select {
	case moved = <-moveDone:
	case <-time.After(3 * time.Second):
		moveBlocked = true
	}
	close(release)
	if moveBlocked {
		select {
		case moved = <-moveDone:
		case <-time.After(5 * time.Second):
			k.Resources.Jobs.Close()
			<-agentDone
			t.Fatal("public profile operation remained blocked after model release")
		}
	}
	profile, moveErr := moved.profile, moved.err
	var got result
	select {
	case got = <-called:
	case <-time.After(5 * time.Second):
		k.Resources.Jobs.Close()
		<-agentDone
		t.Fatal("compact did not return after releasing the model")
	}
	agentErr := <-agentDone
	// A slow machine may have detached the real Shell at its normal two-second
	// wait. Join its actual completion before inspecting State or old paths.
	k.Resources.Jobs.Close()
	if moveBlocked || moveErr != nil || !profile.Moved || profile.Name != name || k.Bot != original || k.currentName() != name || operationEndpoint(k) != endpoint || got.err != nil || agentErr != nil || got.reply.Text != "compacted 1-4" || observed.turn != 23 || len(observed.req.Messages) != 2 || len(observed.req.ToolDefinitions()) != 0 {
		t.Fatalf("model wait/rename lost source or short ACK: heldDuringModel=%v profile=%+v move=%v call=%v agent=%v turn=%d reply=%+v", moveBlocked, profile, moveErr, got.err, agentErr, observed.turn, got.reply)
	}
	compactRuntimeBudget(t, got.reply)
	current, err := k.Resources.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	state, err := current.LoadState()
	if err != nil || len(state.Blocks) != 1 || state.Blocks[0].Text != "RENAMED_CURRENT_STATE_SUMMARY" || state.Blocks[0].Focus != defaultFocus || !state.MemSnapSet {
		t.Fatalf("summary State did not follow current route: blocks=%+v err=%v", state.Blocks, err)
	}
	traces, err := current.Traces()
	if err != nil || len(traces) != 1 || traces[0].Call != bot.TraceCallCompact || traces[0].Turn != 23 || traces[0].Prompt != 97 || traces[0].Completion != 19 {
		t.Fatalf("usage trace did not follow current route: %+v err=%v", traces, err)
	}
	text, err := k.ExpandBot("renamed", 1, 1)
	if err != nil || !strings.Contains(text, "PROJECT_DECISION") {
		t.Fatalf("next original self read lost raw source history: %q err=%v", text, err)
	}
	for _, path := range []string{bot.Dir(original.Root, "source"), work.BotDir(original.Root, "source")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("compact State/usage revived old source path %s: %v", path, err)
		}
	}
}

func TestCompactRuntimeRangeTailPairAndOverlapUseOriginalValidation(t *testing.T) {
	k, endpoint := compactRuntimeFixture(t)
	compactRuntimeSeed(t, k)
	model := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("FIRST_RANGE_SUMMARY"), Usage: provider.TokenUsage{PromptTokens: 31, CompletionTokens: 4, Reported: true}},
		{Role: provider.RoleAssistant, Content: provider.TextContent("COARSER_RANGE_SUMMARY"), Usage: provider.TokenUsage{PromptTokens: 37, CompletionTokens: 5, Reported: true}},
	}}
	k.Compactor = compact.LLM{P: model}
	var replies []cliops.CompactReply
	var callErrors []error
	inputs := []cliops.Compact{
		{From: 0, To: 1}, // domain bounds, not parser positivity
		{From: 1, To: 5}, // the original default eight-entry raw tail
		{From: 2, To: 2}, // one actual unfinished tool pair boundary
		{From: 1, To: 4}, // finished inclusive pair succeeds
		{From: 1, To: 4}, // identical active summary is not compacted again
	}
	runRoutineSource(t, k, nil, `{"command":"compact --self --from 1 --to 4"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		for _, in := range inputs {
			reply, err := cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), in)
			replies = append(replies, reply)
			callErrors = append(callErrors, err)
		}
		return "completed", nil
	})
	if len(replies) != 5 || len(model.reqs) != 1 {
		t.Fatalf("guard failures invoked the LLM: replies=%d calls=%d", len(replies), len(model.reqs))
	}
	for i, piece := range []string{"outside 1-12", "last 8 messages stay raw", "has no result", "", "overlaps active summary 1-4"} {
		compactRuntimeBudget(t, replies[i])
		if i == 3 {
			if callErrors[i] != nil || replies[i].Text != "compacted 1-4" || replies[i].Error != "" {
				t.Fatalf("original inclusive valid range: %+v err=%v", replies[i], callErrors[i])
			}
		} else if callErrors[i] == nil || replies[i].Text != "" || !strings.Contains(replies[i].Error, piece) {
			t.Fatalf("original endpoint guard row %d: %+v err=%v", i, replies[i], callErrors[i])
		}
	}
	// Reuse the domain's full overlap Guardian. Only one strictly wider runtime
	// request is needed here to show it reaches the same prior-summary merger.
	var widened cliops.CompactReply
	var widenedErr error
	runRoutineSource(t, k, nil, `{"command":"compact --self --from 1 --to 5"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		widened, widenedErr = cliops.CallCompact(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.Compact{From: 1, To: 5, Focus: " \n "})
		return "completed", nil
	})
	if widenedErr != nil || widened.Text != "compacted 1-5" || len(model.reqs) != 2 || !strings.Contains(model.reqs[1].Messages[1].Text(), "FIRST_RANGE_SUMMARY") || !strings.Contains(model.reqs[1].Messages[1].Text(), "Focus: "+defaultFocus) {
		t.Fatalf("strictly wider range did not reuse original prior/default focus: %+v calls=%d err=%v", widened, len(model.reqs), widenedErr)
	}
	state, err := k.Bot.LoadState()
	active := ActiveBlocks(state.Blocks)
	if err != nil || len(active) != 1 || active[0].From != 1 || active[0].To != 5 || active[0].Text != "COARSER_RANGE_SUMMARY" {
		t.Fatalf("original coarser active block result: %+v err=%v", active, err)
	}
}
