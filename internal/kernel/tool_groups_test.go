package kernel

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/review"
)

func TestToolGroupColdStart(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona, link.NameTools, link.NameAgents)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ready")}}}
	k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}, link.NewToolsLinkAt(bot.Dir(b.Root, b.Name()), ""), link.Agents{}}, Dispatch: true}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	defs := p.reqs[0].Tools
	if !hasDef(defs, "bot_profile") || !hasDef(defs, "expand_tool_group") {
		t.Fatal("missing discovery entry or expansion tool")
	}
	for _, d := range defs {
		if d.Name == "bot_update_profile" || d.Name == "bot_propose_persona" {
			t.Fatalf("cold request exposes %s", d.Name)
		}
		if d.Name == "bot_profile" {
			if !strings.Contains(d.Description, "Bot资料编辑") || !reflect.DeepEqual(d.Parameters, schema(nil, nil)) {
				t.Fatalf("group entry must be summary-only: %+v", d)
			}
		}
	}
	mounted, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range mounted {
		if tool.Name() != "bot_update_profile" && tool.Name() != "bot_propose_persona" && !hasDef(defs, tool.Name()) {
			t.Errorf("unrelated mounted tool disappeared: %s", tool.Name())
		}
	}
	for _, name := range []string{"write_file", "edit_file", "sh", "dispatch"} {
		if !hasDef(defs, name) {
			t.Errorf("main agent lost %s", name)
		}
	}
	// Compare actual serialized definitions, not a token estimate.
	var full []provider.ToolDef
	for _, tool := range mounted {
		full = append(full, provider.ToolDef{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()})
	}
	before, _ := json.Marshal(full)
	after, _ := json.Marshal(defs)
	t.Logf("tool schema bytes: full=%d cold=%d saved=%d", len(before), len(after), len(before)-len(after))
}

func TestToolGroupExpandProfileAcrossWakes(t *testing.T) {
	for _, permissive := range []bool{false, true} {
		t.Run(map[bool]string{false: "permissions on", true: "permissive"}[permissive], func(t *testing.T) {
			b := testBot(t, link.NameTerminal, link.NamePersona)
			p := &scripted{resps: []provider.Message{
				toolCall("expand", "expand_tool_group", `{"group":"Bot资料编辑"}`),
				toolCall("save", "bot_update_profile", `{"description":"笔记助手","avatar_seed":"Ada#9"}`),
				toolCall("again", "expand_tool_group", `{"group":"bot_profile"}`),
				{Role: provider.RoleAssistant, Content: provider.TextContent("saved")},
				toolCall("persona", "bot_propose_persona", `{"text":"Be kind."}`),
				{Role: provider.RoleAssistant, Content: provider.TextContent("next wake")},
			}}
			var approvals []string
			k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 6, Permissive: permissive,
				Approve: func(_ context.Context, name, _ string) (bool, error) {
					approvals = append(approvals, name)
					return true, nil
				},
			}
			start := time.Now()
			for _, text := range []string{"edit profile", "edit persona"} {
				if err := k.Handle(context.Background(), link.TerminalEvent("u1", text, time.Now())); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("two scripted wakes: %s", time.Since(start))
			if hasDef(p.reqs[0].Tools, "bot_update_profile") {
				t.Fatal("profile exposed before expand")
			}
			for _, req := range p.reqs[1:] {
				for _, name := range []string{"bot_update_profile", "bot_propose_persona", "expand_tool_group"} {
					if !hasDef(req.Tools, name) {
						t.Fatalf("next request lacks %s", name)
					}
				}
				for _, d := range req.Tools {
					if d.Name == "bot_update_profile" && !reflect.DeepEqual(d.Parameters, (profileTool{}).Parameters()) {
						t.Fatal("incomplete profile schema")
					}
				}
			}
			loaded, err := bot.Load(b.Root, b.Name())
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Config().Description != "笔记助手" {
				t.Fatal("profile was not persisted")
			}
			if _, seed := loaded.AvatarState(); seed != "Ada#9" {
				t.Fatal("avatar was not persisted")
			}
			if persona, err := loaded.ReadPersona(); err != nil || strings.TrimSpace(persona) != "Be kind." {
				t.Fatalf("persona %q %v", persona, err)
			}
			want := []string{"bot_update_profile", "bot_propose_persona"}
			if permissive {
				want = nil
			}
			if !reflect.DeepEqual(approvals, want) {
				t.Fatalf("approvals %v, want %v", approvals, want)
			}
			var expansion string
			for _, msg := range p.reqs[1].Messages {
				if msg.Role == provider.RoleTool && msg.Name == "expand_tool_group" {
					expansion = msg.Text()
				}
			}
			var result struct {
				Group string
				Tools []provider.ToolDef
			}
			if err := json.Unmarshal([]byte(expansion), &result); err != nil {
				t.Fatalf("schema result %q: %v", expansion, err)
			}
			if result.Group != "bot_profile" || len(result.Tools) != 2 {
				t.Fatalf("group result %+v", result)
			}
			for _, d := range result.Tools {
				if d.Parameters == nil {
					t.Fatal("missing returned schema")
				}
			}
			var toolUpdates []string
			for _, update := range envUpdates(t, b) {
				if strings.Contains(update, `"tools":`) {
					toolUpdates = append(toolUpdates, update)
				}
			}
			if len(toolUpdates) != 2 || strings.Contains(toolUpdates[0], "bot_update_profile") || !strings.Contains(toolUpdates[1], "bot_update_profile") || !strings.Contains(toolUpdates[1], "bot_propose_persona") {
				t.Fatalf("expected initial tools then one expansion diff: %v", toolUpdates)
			}
			// Restarting the kernel is a fresh discovery session, even with the
			// previous schema response still present in the append-only transcript.
			fresh := &scripted{resps: []provider.Message{toolCall("cold", "bot_update_profile", `{"description":"must not write"}`), {Role: provider.RoleAssistant, Content: provider.TextContent("unavailable")}}}
			restarted := &Kernel{Bot: loaded, Provider: fresh, Links: []link.Link{link.Persona{}}, Permissive: true}
			if err := restarted.Handle(context.Background(), link.TerminalEvent("u1", "restart", time.Now())); err != nil {
				t.Fatal(err)
			}
			if hasDef(fresh.reqs[0].Tools, "bot_update_profile") || loaded.Config().Description != "笔记助手" {
				t.Fatal("expanded state leaked into new kernel")
			}
			if !strings.Contains(messagesText(fresh.reqs[1].Messages), `unknown tool "bot_update_profile"`) {
				t.Fatal("old transcript bypassed discovery")
			}
		})
	}
}

func TestToolGroupUnexpandedCallFails(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona)
	before := profileAvatarFiles(t, b)
	persona, _ := b.ReadPersona()
	p := &scripted{resps: []provider.Message{
		toolCall("profile", "bot_update_profile", `{"description":"must not write"}`),
		toolCall("persona", "bot_propose_persona", `{"text":"must not write"}`),
		toolCall("entry", "bot_profile", `{}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, MaxSteps: 5, Permissive: true}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "edit", time.Now())); err != nil {
		t.Fatal(err)
	}
	text := messagesText(p.reqs[3].Messages)
	for _, want := range []string{`unknown tool "bot_update_profile"`, `unknown tool "bot_propose_persona"`, "is not an action; call expand_tool_group"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing explicit error %s", want)
		}
	}
	afterPersona, _ := b.ReadPersona()
	if !reflect.DeepEqual(before, profileAvatarFiles(t, b)) || persona != afterPersona {
		t.Fatal("unexpanded call wrote profile/persona")
	}
}

func TestToolGroupConcurrentExpandAndInvalidGroups(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NamePersona), Links: []link.Link{link.Persona{}}}
	for _, args := range []string{`{}`, `{"group":""}`, `{"group":1}`, `{"group":"skills"}`, `{"group":"missing"}`} {
		if _, err := (expandToolGroup{k}).Run(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	if len(k.expandedToolGroups) != 0 {
		t.Fatal("invalid group expanded")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := (expandToolGroup{k}).Run(context.Background(), json.RawMessage(`{"group":"bot_profile"}`)); err != nil {
				t.Error(err)
			}
			if _, err := k.modelTools(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(k.expandedToolGroups) != 1 || k.isLoaded("bot_profile") {
		t.Fatal("group/link state mixed or repeated expansion not idempotent")
	}
	for _, name := range []string{"bot_profile", "expand_tool_group"} {
		if review.ClassOf(name) != review.Readonly {
			t.Fatalf("%s lacks discovery classification", name)
		}
	}
	// No persona grant means there is no group to expand, but the meta tool
	// remains available to the main agent.
	other := &Kernel{Bot: testBot(t, link.NameTerminal)}
	list, err := other.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(toolNames(list), "expand_tool_group") {
		t.Fatal("meta tool missing")
	}
	if _, err := (expandToolGroup{other}).Run(context.Background(), json.RawMessage(`{"group":"bot_profile"}`)); err == nil {
		t.Fatal("expanded unmounted group")
	}
}

// Calls in one model response share the tool directory from that request.
// An expansion in the same parallel batch cannot admit a speculative write.
func TestToolGroupExpandDoesNotAdmitSameBatchWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NamePersona)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{
			{ID: "expand", Type: "function", Function: provider.FunctionCall{Name: "expand_tool_group", Arguments: `{"group":"bot_profile"}`}},
			{ID: "early", Type: "function", Function: provider.FunctionCall{Name: "bot_update_profile", Arguments: `{"description":"too soon"}`}},
		})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("expanded")},
	}}
	k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Persona{}}, Permissive: true}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "edit", time.Now())); err != nil {
		t.Fatal(err)
	}
	if b.Config().Description != "" || !strings.Contains(messagesText(p.reqs[1].Messages), `unknown tool "bot_update_profile"`) {
		t.Fatal("same-batch write bypassed discovery")
	}
	if !hasDef(p.reqs[1].Tools, "bot_update_profile") {
		t.Fatal("expansion did not take effect on the next request")
	}
}
