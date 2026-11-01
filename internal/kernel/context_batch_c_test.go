package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/manual"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

func batchCall(id, name, args string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: args}}})}
}
func batchReply(text string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(text)}
}

// These are deterministic integration scripts, not a claim that a real model
// learned the new policy. The scripts exercise actual tool effects and prompt
// delivery; the paired counterexamples below make the acceptance boundary clear.
func TestBatchCManualReadBeforeAnswer(t *testing.T) {
	for _, section := range manual.Sections() {
		t.Run(section.ID, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			body, _ := manual.Read(section.ID)
			p := &scripted{resps: []provider.Message{batchCall("manual", "kernel_manual", fmt.Sprintf(`{"section":%q}`, section.ID)), batchReply(body)}}
			k := &Kernel{Bot: b, Provider: p, MaxSteps: 4}
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", "你怎么做到"+section.Title+"，我在哪看？", time.Now())); err != nil {
				t.Fatal(err)
			}
			if len(p.reqs) != 2 {
				t.Fatalf("requests %d", len(p.reqs))
			}
			prefix := p.reqs[0].Messages[0].Text()
			if !strings.Contains(prefix, manual.Prompt()) || strings.Contains(prefix, body) {
				t.Fatal("prefix needs only manual directory/instruction")
			}
			got := ""
			for _, m := range p.reqs[1].Messages {
				if m.Role == provider.RoleTool {
					got += m.Text()
				}
			}
			if !strings.Contains(got, body) {
				t.Fatal("manual tool content missing before scripted answer")
			}
			if review.ClassOf("kernel_manual") != review.Readonly {
				t.Fatal("manual must be readonly")
			}
		})
	}
	for _, args := range []string{`{}`, `{"section":"../../secret"}`, `{"section":1}`} {
		if _, err := (kernelManualTool{}).Run(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
}

// Replace only process execution: Handle still calls the original Shell through
// Agent -> Jobs, and the parsed command reaches the actual source HTTP handler.
func mountBotCreateSourceShell(t *testing.T, k *Kernel) {
	t.Helper()
	k.Links = append(k.Links, c1Link{tool: routineSourceShell{run: func(ctx context.Context, raw json.RawMessage) (string, error) {
		var input struct{ Command string }
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
		env := map[string]string{}
		for _, item := range k.cliCallEnvironment(ctx, nil) {
			key, value, _ := strings.Cut(item, "=")
			env[key] = value
		}
		if env["XBOT_CALL_ID"] == "" || env["XBOT_CALL_ID"] != tools.CallID(ctx) {
			return "", fmt.Errorf("source Shell has no bound live Job")
		}
		if _, ok := k.liveShell().CallContext(env["XBOT_CALL_ID"]); !ok {
			return "", fmt.Errorf("source Shell Job is not live")
		}
		argv, ok := review.DirectXBotArgs(input.Command, env["XBOT_BIN"])
		if !ok {
			return "", fmt.Errorf("fixture requires a direct source CLI command")
		}
		command, err := cliops.ParseBotRuntime(argv)
		if err != nil {
			return "", err
		}
		if command.Action != "new" || !command.Self || command.PersonaFile != "" {
			return "", fmt.Errorf("fixture requires new --self --persona")
		}
		reply, err := cliops.CallBotCreate(ctx, env["XBOT_ENDPOINT"], env["XBOT_ROOT"], cliops.BotCreate{Name: command.Name, Persona: command.Persona})
		encoded, encodeErr := json.Marshal(reply)
		if err != nil {
			return string(encoded), err
		}
		return string(encoded), encodeErr
	}}})
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
}

func TestBatchCCreateBotPurposeScripts(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		ask         bool
	}{
		{"missing-purpose", "帮我建个 Bot", true},
		{"clear-purpose", "现在建个叫日报助手、负责汇总邮件的 Bot", false},
		{"delegated-name", "现在建一个汇总邮件的 Bot，你定名字", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, err := bot.EnsurePrimary(t.TempDir(), bot.Seed{Links: []string{link.NameWeb}, Owner: "u1"})
			if err != nil {
				t.Fatal(err)
			}
			responses := []provider.Message{}
			if tc.ask {
				responses = append(responses, batchCall("ask", "ask_user", `{"question":"叫什么、负责什么，要现在创建吗？可直接回复用途和名字。","options":["先不建，补充用途和名字","取消"],"recommended":"先不建，补充用途和名字"}`), batchReply("等你补充用途和名字后再建。"))
			}
			createArgs, _ := json.Marshal(map[string]string{"command": `"$XBOT_BIN" new 日报助手 --self --persona '汇总邮件；未经授权不对外发送。' --json`, "description": "创建日报助手"})
			responses = append(responses, batchCall("create", "sh", string(createArgs)), batchReply("已建好日报助手。"))
			p := &scripted{resps: responses}
			k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Web{}}, Permissive: true, MaxSteps: 5}
			mountBotCreateSourceShell(t, k)
			t.Cleanup(func() {
				questionsMu.Lock()
				defer questionsMu.Unlock()
				for key, q := range questions {
					if botKey(q.b) == botKey(b) {
						q.timer.Stop()
						delete(questions, key)
					}
				}
			})
			if err := k.Handle(context.Background(), link.TerminalEvent("u1", tc.input, time.Now())); err != nil {
				t.Fatal(err)
			}
			if tc.ask {
				if _, err := bot.Load(b.Root, "日报助手"); err == nil {
					t.Fatal("created before purpose supplied")
				}
				if p.i != 2 {
					t.Fatalf("did not pause after clarification: %d", p.i)
				}
				if err := k.Handle(context.Background(), link.TerminalEvent("u1", "叫日报助手，汇总邮件，现在建。", time.Now())); err != nil {
					t.Fatal(err)
				}
			}
			made, err := bot.Load(b.Root, "日报助手")
			if err != nil {
				t.Fatal(err)
			}
			persona, err := made.ReadPersona()
			if err != nil || strings.TrimSpace(persona) != "汇总邮件；未经授权不对外发送。" {
				t.Fatalf("persona %q %v", persona, err)
			}
			prompt := p.reqs[0].Messages[0].Text()
			if !strings.Contains(prompt, "可撤销不等于可以替 Bot 编造用途") {
				t.Fatal("creation constraint missing")
			}
			if registered(k, "create_bot") {
				t.Fatal("retired creation tool remains callable")
			}
			entries, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			paired := false
			for _, entry := range entries {
				if entry.Message.Role == provider.RoleTool && entry.Message.CallID() == "create" && entry.Message.Name == "sh" && strings.Contains(entry.Message.Text(), `"applied":true`) {
					paired = true
				}
			}
			if !paired {
				t.Fatal("source CLI create result lost its original Shell call pairing")
			}
		})
	}
}

func TestBatchCCreateRejectsEmptyPurposeWithoutSideEffects(t *testing.T) {
	b, _, err := bot.EnsurePrimary(t.TempDir(), bot.Seed{Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	asks := 0
	k := &Kernel{Bot: b, Approve: func(context.Context, string, string) (bool, error) { asks++; return false, nil }}
	handlers := k.BotHandlers()
	for _, persona := range []string{"", "  "} {
		if reply, err := handlers.Create(context.Background(), cliops.BotCreate{Name: "newbot", Persona: persona}); err == nil || reply.Applied || reply.Stage != "validation" {
			t.Fatalf("empty purpose accepted: %+v %v", reply, err)
		}
		if _, err := os.Stat(bot.Dir(b.Root, "newbot")); !os.IsNotExist(err) {
			t.Fatalf("partial creation: %v", err)
		}
	}
	// Create remains a normal action even on a strict source with an approver.
	if reply, err := handlers.Create(context.Background(), cliops.BotCreate{Name: "newbot", Persona: "take notes"}); err != nil || !reply.Applied || asks != 0 {
		t.Fatalf("unexpected creation card: %+v asks=%d %v", reply, asks, err)
	}
}

func TestBatchCPartnerScenarios(t *testing.T) {
	t.Run("off-topic", func(t *testing.T) {
		b := testBot(t)
		dir := t.TempDir()
		candidate := filepath.Join(dir, "candidates.md")
		p := &scripted{resps: []provider.Message{batchCall("save", "write_file", `{"path":"candidates.md","content":"候选：新增主题换肤；当前先完成导出验收。"}`), batchReply("换肤会延后导出验收，已记入候选，先把导出走通。")}}
		k := &Kernel{Bot: b, Provider: p, WorkDir: dir, Links: []link.Link{c1Link{tool: &tools.WriteFile{Root: dir}}}, Permissive: true, MaxSteps: 4}
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "当前目标是导出验收，顺便再来个换肤吧？新想法记 candidates.md。", time.Now())); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(candidate)
		if err != nil || !strings.Contains(string(raw), "换肤") {
			t.Fatalf("candidate not saved: %s %v", raw, err)
		}
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "新想法让人偏离进行中的目标时，把它记进候选清单") {
			t.Fatal("focus rule missing")
		}
		t.Log("before counterexample: silently starts theme work; after script: records candidate and explains tradeoff")
	})
	t.Run("downstream-self-report", func(t *testing.T) {
		b := testBot(t)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "test.log"), []byte("command: go test ./target\nFAIL\nexit status: 1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		p := &scripted{resps: []provider.Message{batchCall("evidence", "read_file", `{"path":"test.log"}`), batchReply("核对日志后发现测试失败，尚未通过验收。")}}
		k := &Kernel{Bot: b, Provider: p, WorkDir: dir, Links: []link.Link{c1Link{tool: &tools.ReadFile{Root: dir}}}, Permissive: true, MaxSteps: 4}
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "下游说测通了；按约定核对 test.log 的验收证据。", time.Now())); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(messagesText(p.reqs[1].Messages), "exit status: 1") {
			t.Fatal("actual evidence not delivered")
		}
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "只有下游的说法不算验证过") {
			t.Fatal("verification rule missing")
		}
		t.Log("before counterexample: repeats passed without checking; after script: reads failing evidence and reports failure")
	})
	t.Run("unclear-goal", func(t *testing.T) {
		b := testBot(t)
		dir := t.TempDir()
		p := &scripted{resps: []provider.Message{batchReply("你说的优化，怎样才算做好：响应更快还是操作更少？")}}
		k := &Kernel{Bot: b, Provider: p, WorkDir: dir, MaxSteps: 2}
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "把它优化一下，直接开始。", time.Now())); err != nil {
			t.Fatal(err)
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 0 {
			t.Fatalf("started implementation without goal: %v %v", files, err)
		}
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "开工前先对齐怎样算做完") {
			t.Fatal("goal rule missing")
		}
		t.Log("before counterexample: guesses optimization target and edits; after script: clarifies outcome before writing")
	})
}

func TestBatchCEvidenceFiveTasksUsesEnvironment(t *testing.T) {
	hold := make(chan struct{})
	p := &batchTaskCounter{t: t, hold: hold}
	k, b, _ := dispatchKernel(t, p)
	// Task status needs no web/shell tools. Keep the executor and main-line
	// provider fixed before starting workers; do not mutate a live kernel.
	k.Links = []link.Link{link.Terminal{}, link.Agents{}}
	defer finishAdmissionJobs(t, k, hold)
	for i := 0; i < 5; i++ {
		if _, err := k.dispatch(context.Background(), "research", fmt.Sprintf("调研 %d", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "现在总共几项，各是什么状态？", time.Now())); err != nil {
		t.Fatal(err)
	}
	if p.total != 5 || p.running != 3 || p.queued != 2 {
		t.Fatalf("environment tasks total=%d running=%d queued=%d", p.total, p.running, p.queued)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !transcriptHas(entries, "共 5 项，3 项进行中，2 项排队。") {
		t.Fatal("wrong task count in reply")
	}
}

type batchTaskCounter struct {
	hold                   chan struct{}
	t                      *testing.T
	total, running, queued int
}

func (p *batchTaskCounter) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Text(), exec.SubPersona) {
		<-p.hold
		return batchReply("Findings\nnone"), nil
	}
	if !strings.Contains(req.Messages[0].Text(), "任务数和状态按最新回放的环境来数") {
		return provider.Message{}, fmt.Errorf("task evidence rule absent")
	}
	env := replayEnvironment(req.Messages, "")
	for key, raw := range env.Fields {
		if !strings.HasPrefix(key, "task:") {
			continue
		}
		var task bot.TaskEnv
		if err := json.Unmarshal(raw, &task); err != nil {
			return provider.Message{}, err
		}
		p.total++
		switch task.Status {
		case "running":
			p.running++
		case "queued":
			p.queued++
		}
	}
	return batchReply(fmt.Sprintf("共 %d 项，%d 项进行中，%d 项排队。", p.total, p.running, p.queued)), nil
}

func TestBatchCEvidenceValueIsNotTestProof(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{batchReply("收到验收值 731010；没有测试命令、输出和退出状态，测试是否通过尚未验证。")}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	if err := k.Handle(context.Background(), link.NewEvent(link.SourceSubagent, "done", "验收值731010；已完成。", "sa_value", time.Now())); err != nil {
		t.Fatal(err)
	}
	req := p.reqs[0]
	if !strings.Contains(req.Messages[0].Text(), "只有验收数值、没有测试细节，不代表测试跑过或通过") {
		t.Fatal("evidence constraint absent")
	}
	if !strings.Contains(messagesText(req.Messages), "验收值731010；已完成。") {
		t.Fatal("ordinary result event lost")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if !transcriptHas(entries, "测试是否通过尚未验证") {
		t.Fatal("scripted qualification lost")
	}
}

func TestBatchCRulesUpgradePreservesDutiesAndWorkingMemory(t *testing.T) {
	b := testBot(t)
	persona, err := b.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	// Openings are preserved so old defaults disappear, without a historical
	// paragraph registry or touching the owner's separate persona/memory files.
	old := []string{
		"When you lack a tool or information, do the part you can first. Old advice.",
		"Lead with the answer, then give the reason. Old judgment.",
		"Report only outcomes supported by tool or subagent results; distinguish unfinished work. Old verification.",
		"If you offer the owner a choice and they ignore it or change the subject, do not repeat that offer. Continue with what they asked.",
	}
	if err := b.WriteRules(strings.Join(old, "\n\n") + "\n\nOWNER_SUPPLEMENT"); err != nil {
		t.Fatal(err)
	}
	mem := filepath.Join(bot.Dir(b.Root, b.Name()), "memory", "memory.md")
	if err := os.MkdirAll(filepath.Dir(mem), 0700); err != nil {
		t.Fatal(err)
	}
	working := "# Bot memory\n\n## 索引\n\n## 常驻\n- (2026-10-26) 正在做导出验收\n"
	if err := os.WriteFile(mem, []byte(working), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		k := &Kernel{Bot: b}
		rules, err := k.rulesText()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rules, manual.Prompt()) || !strings.Contains(rules, "OWNER_SUPPLEMENT") || strings.Contains(rules, "Old advice") {
			t.Fatal("upgrade failed")
		}
	}
	got, err := os.ReadFile(mem)
	if err != nil || string(got) != working {
		t.Fatal("upgrade changed working memory")
	}
	gotPersona, err := b.ReadPersona()
	if err != nil || gotPersona != persona {
		t.Fatal("upgrade changed owner duties")
	}
}
