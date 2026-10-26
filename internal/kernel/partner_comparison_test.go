package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Before/after are authored behavioral fixtures, not recordings of real-model
// runs. Running both through actual tools checks that our acceptance predicates
// reject the counterexample (including a convincing success claim). Prompt
// delivery is covered separately; this test cannot establish model improvement.
func TestBatchCPartnerBehaviorComparison(t *testing.T) {
	cases := []struct {
		name, input   string
		before, after []provider.Message
		accept        func(string, []bot.Entry) bool
	}{
		{"off-topic", "当前验收导出，新点子换肤记 candidates.md，别改当前方向。",
			[]provider.Message{batchCall("write", "write_file", `{"path":"theme.txt","content":"开始换肤"}`), batchReply("开始换肤。")},
			[]provider.Message{batchCall("write", "write_file", `{"path":"candidates.md","content":"候选：换肤；本次先验收导出。"}`), batchReply("已记候选，换肤会延后验收，先完成导出。")},
			func(dir string, entries []bot.Entry) bool {
				raw, err := os.ReadFile(filepath.Join(dir, "candidates.md"))
				_, theme := os.Stat(filepath.Join(dir, "theme.txt"))
				return err == nil && strings.Contains(string(raw), "换肤") && os.IsNotExist(theme) && transcriptHas(entries, "先完成导出")
			}},
		{"downstream-claim", "下游自报已测通，请按约定核对 test.log 再汇报。",
			[]provider.Message{batchReply("已测试通过。")},
			[]provider.Message{batchCall("read", "read_file", `{"path":"test.log"}`), batchReply("日志里 exit status: 1，测试失败，不能验收。")},
			func(_ string, entries []bot.Entry) bool {
				read := false
				for _, e := range entries {
					if e.Message.Role == provider.RoleTool && strings.Contains(e.Message.Text(), "exit status: 1") {
						read = true
					}
				}
				return read && transcriptHas(entries, "测试失败") && !transcriptHas(entries, "已测试通过")
			}},
		{"unclear-goal", "把它优化一下，直接开工。",
			[]provider.Message{batchCall("write", "write_file", `{"path":"optimization.txt","content":"猜测要改颜色"}`), batchReply("已优化。")},
			[]provider.Message{batchReply("怎样才算优化好：响应更快，还是操作更少？")},
			func(dir string, entries []bot.Entry) bool {
				_, err := os.Stat(filepath.Join(dir, "optimization.txt"))
				return os.IsNotExist(err) && transcriptHas(entries, "怎样才算优化好")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, variant := range []struct {
				name      string
				responses []provider.Message
				want      bool
			}{{"before-counterexample", tc.before, false}, {"after-contract", tc.after, true}} {
				t.Run(variant.name, func(t *testing.T) {
					dir := t.TempDir()
					if err := os.WriteFile(filepath.Join(dir, "test.log"), []byte("command: go test ./target\nFAIL\nexit status: 1\n"), 0600); err != nil {
						t.Fatal(err)
					}
					p := &scripted{resps: variant.responses}
					b := testBot(t)
					k := &Kernel{Bot: b, Provider: p, WorkDir: dir, Links: []link.Link{link.NewToolsLinkAt(dir, "")}, Permissive: true, MaxSteps: 4}
					if err := k.Handle(context.Background(), link.TerminalEvent("u1", tc.input, time.Now())); err != nil {
						t.Fatal(err)
					}
					entries, err := b.Entries()
					if err != nil {
						t.Fatal(err)
					}
					if got := tc.accept(dir, entries); got != variant.want {
						t.Fatalf("behavior acceptance=%v want=%v", got, variant.want)
					}
					t.Logf("scripted behavioral acceptance: %v", variant.want)
				})
			}
		})
	}
}
