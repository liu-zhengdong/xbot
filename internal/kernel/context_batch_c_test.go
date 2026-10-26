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
			responses = append(responses, batchCall("create", "create_bot", `{"name":"日报助手","persona":"汇总邮件；未经授权不对外发送。"}`), batchReply("已建好日报助手。"))
			p := &scripted{resps: responses}
			k := &Kernel{Bot: b, Provider: p, Links: []link.Link{link.Web{}}, Permissive: true, MaxSteps: 5}
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
			if !strings.Contains(prompt, "Reversibility is not permission to invent") {
				t.Fatal("creation constraint missing")
			}
			found := false
			for _, tool := range p.reqs[0].Tools {
				if tool.Name == "create_bot" {
					found = strings.Contains(tool.Description, "Do not call when its purpose or duties are unclear")
				}
			}
			if !found {
				t.Fatal("creation schema/description not delivered")
			}
			if review.ClassOf("create_bot") != review.Normal {
				t.Fatal("creation became a permission gate")
			}
		})
	}
}

func TestBatchCCreateRejectsEmptyPurposeWithoutSideEffects(t *testing.T) {
	b, _, err := bot.EnsurePrimary(t.TempDir(), bot.Seed{Owner: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	for _, args := range []string{`{"name":"newbot"}`, `{"name":"newbot","persona":"  "}`} {
		if _, err := (createBotTool{k}).Run(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatal("empty purpose accepted")
		}
		if _, err := os.Stat(bot.Dir(b.Root, "newbot")); !os.IsNotExist(err) {
			t.Fatalf("partial creation: %v", err)
		}
	}
	// With permissions enabled this is still a normal action, never a card.
	if need, _, err := (reviewedTool{Tool: createBotTool{k}, k: k}).Confirm(json.RawMessage(`{"name":"newbot","persona":"take notes"}`)); err != nil || need {
		t.Fatalf("unexpected card: %v %v", need, err)
	}
}

func TestBatchCPartnerScenarios(t *testing.T) {
	t.Run("off-topic", func(t *testing.T) {
		b := testBot(t)
		dir := t.TempDir()
		candidate := filepath.Join(dir, "candidates.md")
		p := &scripted{resps: []provider.Message{batchCall("save", "write_file", `{"path":"candidates.md","content":"候选：新增主题换肤；当前先完成导出验收。"}`), batchReply("换肤会延后导出验收，已记入候选，先把导出走通。")}}
		k := &Kernel{Bot: b, Provider: p, WorkDir: dir, Links: []link.Link{oneToolLink{t: &tools.WriteFile{Root: dir}}}, Permissive: true, MaxSteps: 4}
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "当前目标是导出验收，顺便再来个换肤吧？新想法记 candidates.md。", time.Now())); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(candidate)
		if err != nil || !strings.Contains(string(raw), "换肤") {
			t.Fatalf("candidate not saved: %s %v", raw, err)
		}
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "When a new idea distracts from an active goal") {
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
		k := &Kernel{Bot: b, Provider: p, WorkDir: dir, Links: []link.Link{oneToolLink{t: &tools.ReadFile{Root: dir}}}, Permissive: true, MaxSteps: 4}
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", "下游说测通了；按约定核对 test.log 的验收证据。", time.Now())); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(messagesText(p.reqs[1].Messages), "exit status: 1") {
			t.Fatal("actual evidence not delivered")
		}
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "a downstream claim alone is unverified") {
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
		if !strings.Contains(p.reqs[0].Messages[0].Text(), "Before starting work, align on what done looks like") {
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
	if !strings.Contains(req.Messages[0].Text(), "Count tasks and statuses from the latest replayed environment") {
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
	if !strings.Contains(req.Messages[0].Text(), "An acceptance value without test details does not mean tests ran or passed") {
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
